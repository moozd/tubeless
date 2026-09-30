// Pixel plumbing: per-surface images, the composited canvas, and the
// shared-memory buffer the window reads frames from.

use std::{os::fd::OwnedFd, ptr::null_mut};

use rustix::{
	fs::{ftruncate, memfd_create, MemfdFlags},
	mm::{mmap, munmap, MapFlags, ProtFlags},
};

#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub struct Rect {
	pub x: i32,
	pub y: i32,
	pub w: i32,
	pub h: i32,
}

impl Rect {
	pub fn is_empty(&self) -> bool {
		self.w <= 0 || self.h <= 0
	}

	pub fn union(self, o: Rect) -> Rect {
		if self.is_empty() {
			return o;
		}
		if o.is_empty() {
			return self;
		}
		let x0 = self.x.min(o.x);
		let y0 = self.y.min(o.y);
		let x1 = (self.x + self.w).max(o.x + o.w);
		let y1 = (self.y + self.h).max(o.y + o.h);
		Rect {
			x: x0,
			y: y0,
			w: x1 - x0,
			h: y1 - y0,
		}
	}

	pub fn clip(self, w: i32, h: i32) -> Rect {
		let x0 = self.x.max(0);
		let y0 = self.y.max(0);
		let x1 = (self.x + self.w).min(w);
		let y1 = (self.y + self.h).min(h);
		Rect {
			x: x0,
			y: y0,
			w: (x1 - x0).max(0),
			h: (y1 - y0).max(0),
		}
	}
}

// BGRA, alpha already forced to 255 for Xrgb buffers.
#[derive(Default)]
pub struct SurfaceImage {
	pub w: i32,
	pub h: i32,
	pub data: Vec<u8>,
}

pub struct Canvas {
	pub w: i32,
	pub h: i32,
	pub px: Vec<u8>,
}

impl Canvas {
	pub fn new(w: i32, h: i32) -> Self {
		let mut c = Self {
			w,
			h,
			px: vec![0; (w * h * 4) as usize],
		};
		c.clear();
		c
	}

	pub fn clear(&mut self) {
		for p in self.px.chunks_exact_mut(4) {
			p.copy_from_slice(&[0, 0, 0, 255]);
		}
	}

	// Source-over of premultiplied BGRA onto an opaque canvas.
	pub fn blit(&mut self, img: &SurfaceImage, at_x: i32, at_y: i32) {
		for sy in 0..img.h {
			let dy = at_y + sy;
			if dy < 0 || dy >= self.h {
				continue;
			}
			for sx in 0..img.w {
				let dx = at_x + sx;
				if dx < 0 || dx >= self.w {
					continue;
				}
				let s = ((sy * img.w + sx) * 4) as usize;
				let d = ((dy * self.w + dx) * 4) as usize;
				let inv = 255 - img.data[s + 3] as u32;
				for c in 0..3 {
					let v = img.data[s + c] as u32 + self.px[d + c] as u32 * inv / 255;
					self.px[d + c] = v.min(255) as u8;
				}
			}
		}
	}
}

pub struct Shm {
	pub fd: OwnedFd,
	ptr: *mut u8,
	len: usize,
	pub w: i32,
	pub h: i32,
}

impl Shm {
	pub fn new(w: i32, h: i32) -> rustix::io::Result<Self> {
		let len = (w * h * 4) as usize;
		let fd = memfd_create("tubeless-frame", MemfdFlags::CLOEXEC)?;
		ftruncate(&fd, len as u64)?;
		let ptr = unsafe {
			mmap(
				null_mut(),
				len,
				ProtFlags::READ | ProtFlags::WRITE,
				MapFlags::SHARED,
				&fd,
				0,
			)?
		};
		Ok(Self {
			fd,
			ptr: ptr as *mut u8,
			len,
			w,
			h,
		})
	}

	// Copies the rect's rows from the canvas into the shared buffer.
	pub fn write_rect(&mut self, canvas: &Canvas, r: Rect) {
		let dst = unsafe { std::slice::from_raw_parts_mut(self.ptr, self.len) };
		for y in r.y..r.y + r.h {
			let i = ((y * self.w + r.x) * 4) as usize;
			let n = (r.w * 4) as usize;
			dst[i..i + n].copy_from_slice(&canvas.px[i..i + n]);
		}
	}
}

impl Drop for Shm {
	fn drop(&mut self) {
		if let Err(e) = unsafe { munmap(self.ptr as *mut _, self.len) } {
			eprintln!("tubeless-wm: munmap: {e}");
		}
	}
}

#[cfg(test)]
mod tests {
	use super::*;

	#[test]
	fn union_ignores_empty_rects_and_covers_both() {
		let a = Rect {
			x: 10,
			y: 10,
			w: 5,
			h: 5,
		};
		assert_eq!(Rect::default().union(a), a);
		let b = Rect {
			x: 0,
			y: 12,
			w: 4,
			h: 10,
		};
		assert_eq!(
			a.union(b),
			Rect {
				x: 0,
				y: 10,
				w: 15,
				h: 12
			}
		);
	}

	#[test]
	fn clip_keeps_only_the_part_inside_the_canvas() {
		let r = Rect {
			x: -5,
			y: 8,
			w: 20,
			h: 20,
		};
		assert_eq!(
			r.clip(10, 10),
			Rect {
				x: 0,
				y: 8,
				w: 10,
				h: 2
			}
		);
		assert!(Rect {
			x: 20,
			y: 0,
			w: 5,
			h: 5
		}
		.clip(10, 10)
		.is_empty());
	}

	#[test]
	fn blit_blends_premultiplied_pixels_over_the_canvas() {
		let mut canvas = Canvas::new(2, 1);
		let opaque = SurfaceImage {
			w: 1,
			h: 1,
			data: vec![10, 20, 30, 255],
		};
		canvas.blit(&opaque, 0, 0);
		assert_eq!(&canvas.px[0..4], &[10, 20, 30, 255]);
		let clear = SurfaceImage {
			w: 1,
			h: 1,
			data: vec![0, 0, 0, 0],
		};
		canvas.blit(&clear, 0, 0);
		assert_eq!(&canvas.px[0..4], &[10, 20, 30, 255]);
		canvas.blit(&opaque, 1, 0);
		assert_eq!(&canvas.px[4..8], &[10, 20, 30, 255]);
	}
}
