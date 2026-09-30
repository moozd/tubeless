// Wire protocol between the tubeless window and this compositor. One
// SOCK_SEQPACKET message per packet: a type byte, then little-endian
// fields. Frames travel through a shared memfd (sent once, with
// SCM_RIGHTS) and a Frame message names the damaged rect inside it.

use std::{
	io::{IoSlice, IoSliceMut},
	os::fd::{AsFd, BorrowedFd},
};

use rustix::net::{
	recvmsg, sendmsg, RecvAncillaryBuffer, RecvFlags, SendAncillaryBuffer, SendAncillaryMessage,
	SendFlags,
};

pub const VERSION: u8 = 1;

const MAX_PACKET: usize = 4096;

pub enum FromWindow {
	Resize { w: u32, h: u32 },
	PointerMove { x: i32, y: i32 },
	PointerButton { button: u8, pressed: bool },
	PointerAxis { dx: i32, dy: i32 },
	Key { keysym: u32, pressed: bool },
	Focus { focused: bool },
}

pub enum ToWindow<'a> {
	Hello,
	Buffer {
		w: u32,
		h: u32,
		stride: u32,
		fd: BorrowedFd<'a>,
	},
	Frame {
		x: u32,
		y: u32,
		w: u32,
		h: u32,
	},
	Exited {
		code: i32,
	},
}

fn u32_at(b: &[u8], i: usize) -> Option<u32> {
	Some(u32::from_le_bytes(b.get(i..i + 4)?.try_into().ok()?))
}

fn i32_at(b: &[u8], i: usize) -> Option<i32> {
	Some(i32::from_le_bytes(b.get(i..i + 4)?.try_into().ok()?))
}

pub fn decode(b: &[u8]) -> Option<FromWindow> {
	let (&kind, _) = b.split_first()?;
	match kind {
		0x01 => Some(FromWindow::Resize {
			w: u32_at(b, 1)?,
			h: u32_at(b, 5)?,
		}),
		0x02 => Some(FromWindow::PointerMove {
			x: i32_at(b, 1)?,
			y: i32_at(b, 5)?,
		}),
		0x03 => Some(FromWindow::PointerButton {
			button: *b.get(1)?,
			pressed: *b.get(2)? != 0,
		}),
		0x04 => Some(FromWindow::PointerAxis {
			dx: i32_at(b, 1)?,
			dy: i32_at(b, 5)?,
		}),
		0x05 => Some(FromWindow::Key {
			keysym: u32_at(b, 1)?,
			pressed: *b.get(5)? != 0,
		}),
		0x06 => Some(FromWindow::Focus {
			focused: *b.get(1)? != 0,
		}),
		_ => None,
	}
}

fn encode(msg: &ToWindow) -> Vec<u8> {
	let mut out = Vec::new();
	match msg {
		ToWindow::Hello => out.extend([0x81, VERSION]),
		ToWindow::Buffer { w, h, stride, .. } => {
			out.push(0x82);
			out.extend(w.to_le_bytes());
			out.extend(h.to_le_bytes());
			out.extend(stride.to_le_bytes());
		}
		ToWindow::Frame { x, y, w, h } => {
			out.push(0x83);
			for v in [x, y, w, h] {
				out.extend(v.to_le_bytes());
			}
		}
		ToWindow::Exited { code } => {
			out.push(0x85);
			out.extend(code.to_le_bytes());
		}
	}
	out
}

pub fn send(sock: impl AsFd, msg: &ToWindow) -> rustix::io::Result<()> {
	let bytes = encode(msg);
	let iov = [IoSlice::new(&bytes)];
	let mut space = [std::mem::MaybeUninit::uninit(); rustix::cmsg_space!(ScmRights(1))];
	let mut ancillary = SendAncillaryBuffer::new(&mut space);
	let fds;
	if let ToWindow::Buffer { fd, .. } = msg {
		fds = [*fd];
		ancillary.push(SendAncillaryMessage::ScmRights(&fds));
	}
	sendmsg(sock, &iov, &mut ancillary, SendFlags::empty())?;
	Ok(())
}

pub enum Recv {
	Message(FromWindow),
	Unknown,
	Drained,
}

// Reads one packet. Drained means EAGAIN; an empty packet is EOF and
// reported as Err(ECONNRESET).
pub fn recv(sock: impl AsFd) -> rustix::io::Result<Recv> {
	let mut buf = [0u8; MAX_PACKET];
	let mut space = [std::mem::MaybeUninit::uninit(); rustix::cmsg_space!(ScmRights(1))];
	let mut ancillary = RecvAncillaryBuffer::new(&mut space);
	let mut iov = [IoSliceMut::new(&mut buf)];
	match recvmsg(sock, &mut iov, &mut ancillary, RecvFlags::DONTWAIT) {
		Ok(r) if r.bytes == 0 => Err(rustix::io::Errno::CONNRESET),
		Ok(r) => Ok(decode(&buf[..r.bytes]).map_or(Recv::Unknown, Recv::Message)),
		Err(rustix::io::Errno::AGAIN) => Ok(Recv::Drained),
		Err(e) => Err(e),
	}
}

#[cfg(test)]
mod tests {
	use super::*;

	#[test]
	fn decodes_each_window_message() {
		let mut key = vec![0x05];
		key.extend(0x01000041u32.to_le_bytes());
		key.push(1);
		assert!(matches!(
			decode(&key),
			Some(FromWindow::Key {
				keysym: 0x01000041,
				pressed: true
			})
		));
		let mut resize = vec![0x01];
		resize.extend(640u32.to_le_bytes());
		resize.extend(400u32.to_le_bytes());
		assert!(matches!(
			decode(&resize),
			Some(FromWindow::Resize { w: 640, h: 400 })
		));
		let mut motion = vec![0x02];
		motion.extend((-3i32).to_le_bytes());
		motion.extend(512i32.to_le_bytes());
		assert!(matches!(
			decode(&motion),
			Some(FromWindow::PointerMove { x: -3, y: 512 })
		));
		assert!(matches!(
			decode(&[0x03, 3, 1]),
			Some(FromWindow::PointerButton {
				button: 3,
				pressed: true
			})
		));
		assert!(matches!(
			decode(&[0x06, 1]),
			Some(FromWindow::Focus { focused: true })
		));
	}

	#[test]
	fn rejects_truncated_and_unknown_messages() {
		assert!(decode(&[]).is_none());
		assert!(decode(&[0x01, 1, 2]).is_none());
		assert!(decode(&[0x7f]).is_none());
	}
}
