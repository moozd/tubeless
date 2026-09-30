// tubeless-wm: a single-app headless Wayland compositor. The tubeless
// window spawns one per embedded app, hands it a socketpair (--fd), and
// receives composited frames through a shared memfd (see proto.rs).

mod frame;
mod keys;
mod proto;

use std::{
	cell::RefCell,
	collections::HashMap,
	os::fd::{FromRawFd, OwnedFd},
	process::Command,
	sync::Arc,
	time::{Duration, Instant},
};

use smithay::{
	backend::input::{Axis, AxisSource, ButtonState, KeyState},
	delegate_compositor, delegate_data_device, delegate_output, delegate_seat, delegate_shm,
	delegate_xdg_decoration, delegate_xdg_shell,
	input::{
		keyboard::{FilterResult, XkbConfig},
		pointer::{AxisFrame, ButtonEvent, MotionEvent},
		Seat, SeatHandler, SeatState,
	},
	output::{Mode, Output, PhysicalProperties, Subpixel},
	reexports::{
		calloop::{
			channel::{channel, Event as ChannelEvent},
			generic::Generic,
			timer::{TimeoutAction, Timer},
			EventLoop, Interest, LoopSignal, Mode as IoMode, PostAction,
		},
		wayland_protocols::xdg::{
			decoration::zv1::server::zxdg_toplevel_decoration_v1::Mode as DecorationMode,
			shell::server::xdg_toplevel,
		},
		wayland_server::{
			backend::{ClientData, ClientId, DisconnectReason},
			protocol::{wl_buffer, wl_callback::WlCallback, wl_seat, wl_surface::WlSurface},
			Client, Display, DisplayHandle, ListeningSocket, Resource,
		},
	},
	utils::{Logical, Point, Rectangle, Serial, SERIAL_COUNTER},
	wayland::{
		buffer::BufferHandler,
		compositor::{
			get_parent, is_sync_subsurface, with_states, with_surface_tree_downward,
			BufferAssignment, CompositorClientState, CompositorHandler, CompositorState,
			SubsurfaceCachedState, SurfaceAttributes, TraversalAction, SUBSURFACE_ROLE,
		},
		output::OutputHandler,
		selection::{
			data_device::{
				ClientDndGrabHandler, DataDeviceHandler, DataDeviceState, ServerDndGrabHandler,
			},
			SelectionHandler,
		},
		shell::xdg::{
			decoration::{XdgDecorationHandler, XdgDecorationState},
			PopupSurface, PositionerState, ToplevelSurface, XdgShellHandler, XdgShellState,
		},
		shm::{with_buffer_contents, ShmHandler, ShmState},
	},
};

use frame::{Canvas, Rect, Shm, SurfaceImage};
use keys::KeyLookup;
use proto::{FromWindow, Recv, ToWindow};

const FRAME_INTERVAL: Duration = Duration::from_millis(16);
const EVDEV_BTN_LEFT: u32 = 0x110;

struct Popup {
	surface: PopupSurface,
	rel: Point<i32, Logical>,
}

struct State {
	dh: DisplayHandle,
	compositor: CompositorState,
	xdg_shell: XdgShellState,
	_decoration: XdgDecorationState,
	shm: ShmState,
	seat_state: SeatState<State>,
	seat: Seat<State>,
	data_device: DataDeviceState,
	output: Output,
	start: Instant,
	signal: LoopSignal,

	sock: OwnedFd,
	canvas: Canvas,
	shm_out: Option<Shm>,
	keys: KeyLookup,

	toplevels: Vec<ToplevelSurface>,
	popups: Vec<Popup>,
	callbacks: Vec<WlCallback>,
	dirty: Vec<u32>,
	prev_rects: HashMap<u32, Rect>,
	full_damage: bool,
	shift_held: bool,
	focused: bool,
	pointer: Point<f64, Logical>,
}

struct AppClient {
	compositor: CompositorClientState,
}

impl ClientData for AppClient {
	fn initialized(&self, _: ClientId) {}
	fn disconnected(&self, _: ClientId, _: DisconnectReason) {}
}

fn surface_key(s: &WlSurface) -> u32 {
	s.id().protocol_id()
}

impl State {
	fn now_ms(&self) -> u32 {
		self.start.elapsed().as_millis() as u32
	}

	fn notify(&self, msg: &ToWindow) {
		if let Err(e) = proto::send(&self.sock, msg) {
			eprintln!("tubeless-wm: send to window: {e}");
		}
	}

	// Copies every freshly committed buffer in the tree into its
	// surface's stored image, and queues its frame callbacks.
	fn absorb_tree(&mut self, root: &WlSurface) {
		let mut dirty = Vec::new();
		let mut callbacks = Vec::new();
		with_surface_tree_downward(
			root,
			(),
			|_, _, _| TraversalAction::DoChildren(()),
			|surface, states, _| {
				let (assignment, cbs) = {
					let mut cached = states.cached_state.get::<SurfaceAttributes>();
					let attrs = cached.current();
					(
						attrs.buffer.take(),
						std::mem::take(&mut attrs.frame_callbacks),
					)
				};
				callbacks.extend(cbs);
				let cell = states
					.data_map
					.get_or_insert(|| RefCell::new(SurfaceImage::default()));
				match assignment {
					Some(BufferAssignment::NewBuffer(buf)) => {
						copy_buffer(&buf, &mut cell.borrow_mut());
						buf.release();
						dirty.push(surface_key(surface));
					}
					Some(BufferAssignment::Removed) => {
						*cell.borrow_mut() = SurfaceImage::default();
						dirty.push(surface_key(surface));
					}
					None => {}
				}
			},
			|_, _, _| true,
		);
		self.dirty.extend(dirty);
		self.callbacks.extend(callbacks);
	}

	fn surface_size(&self, s: &WlSurface) -> (i32, i32) {
		with_states(s, |states| {
			states
				.data_map
				.get::<RefCell<SurfaceImage>>()
				.map_or((0, 0), |c| (c.borrow().w, c.borrow().h))
		})
	}

	// Top-level surfaces, dialogs and popups with their canvas origin,
	// bottom to top. Dialogs are centered; popups sit at their parent's
	// origin plus their (constrained) geometry.
	fn roots(&self) -> Vec<(WlSurface, Point<i32, Logical>)> {
		let mut out: Vec<(WlSurface, Point<i32, Logical>)> = Vec::new();
		for (i, t) in self.toplevels.iter().enumerate() {
			let s = t.wl_surface().clone();
			let origin = if i == 0 {
				Point::from((0, 0))
			} else {
				let (w, h) = self.surface_size(&s);
				Point::from(((self.canvas.w - w) / 2, (self.canvas.h - h) / 2))
			};
			out.push((s, origin));
		}
		for p in &self.popups {
			let parent = p.surface.get_parent_surface();
			let base = parent
				.and_then(|par| out.iter().find(|(s, _)| *s == par).map(|(_, o)| *o))
				.unwrap_or_default();
			out.push((p.surface.wl_surface().clone(), base + p.rel));
		}
		out
	}

	// Recomposites the canvas and ships the damaged rect to the window.
	fn render(&mut self) {
		let roots = self.roots();
		self.canvas.clear();
		let mut rects: HashMap<u32, Rect> = HashMap::new();
		for (root, origin) in &roots {
			composite_tree(&mut self.canvas, root, *origin, &mut rects);
		}

		let mut damage = Rect::default();
		if self.full_damage {
			damage = Rect {
				x: 0,
				y: 0,
				w: self.canvas.w,
				h: self.canvas.h,
			};
		}
		for key in self.dirty.drain(..) {
			if let Some(r) = rects.get(&key) {
				damage = damage.union(*r);
			}
			if let Some(r) = self.prev_rects.get(&key) {
				damage = damage.union(*r);
			}
		}
		self.prev_rects = rects;
		self.full_damage = false;
		let damage = damage.clip(self.canvas.w, self.canvas.h);
		if damage.is_empty() {
			return;
		}
		self.ship(damage);
	}

	fn ship(&mut self, damage: Rect) {
		let stale = self
			.shm_out
			.as_ref()
			.map_or(true, |s| s.w != self.canvas.w || s.h != self.canvas.h);
		if stale {
			match Shm::new(self.canvas.w, self.canvas.h) {
				Ok(shm) => {
					use std::os::fd::AsFd;
					self.notify(&ToWindow::Buffer {
						w: shm.w as u32,
						h: shm.h as u32,
						stride: shm.w as u32 * 4,
						fd: shm.fd.as_fd(),
					});
					self.shm_out = Some(shm);
				}
				Err(e) => {
					eprintln!("tubeless-wm: allocate frame buffer: {e}");
					return;
				}
			}
		}
		let whole = Rect {
			x: 0,
			y: 0,
			w: self.canvas.w,
			h: self.canvas.h,
		};
		let damage = if stale { whole } else { damage };
		if let Some(shm) = self.shm_out.as_mut() {
			shm.write_rect(&self.canvas, damage);
		}
		self.notify(&ToWindow::Frame {
			x: damage.x as u32,
			y: damage.y as u32,
			w: damage.w as u32,
			h: damage.h as u32,
		});
	}

	fn resize(&mut self, w: i32, h: i32) {
		let (w, h) = (w.max(1), h.max(1));
		if w == self.canvas.w && h == self.canvas.h {
			return;
		}
		self.canvas = Canvas::new(w, h);
		let mode = Mode {
			size: (w, h).into(),
			refresh: 60_000,
		};
		self.output
			.change_current_state(Some(mode), None, None, None);
		self.output.set_preferred(mode);
		for t in &self.toplevels {
			configure_toplevel(t, w, h, self.focused);
		}
		self.full_damage = true;
		self.render();
	}

	fn set_focus(&mut self, focused: bool) {
		self.focused = focused;
		let (w, h) = (self.canvas.w, self.canvas.h);
		for t in &self.toplevels {
			configure_toplevel(t, w, h, focused);
		}
		let target = self.toplevels.last().map(|t| t.wl_surface().clone());
		let Some(kb) = self.seat.get_keyboard() else {
			return;
		};
		let serial = SERIAL_COUNTER.next_serial();
		kb.set_focus(self, if focused { target } else { None }, serial);
	}

	fn pointer_target(&self, p: Point<f64, Logical>) -> Option<(WlSurface, Point<f64, Logical>)> {
		for (s, origin) in self.roots().into_iter().rev() {
			let (w, h) = self.surface_size(&s);
			let inside = p.x >= origin.x as f64
				&& p.y >= origin.y as f64
				&& p.x < (origin.x + w) as f64
				&& p.y < (origin.y + h) as f64;
			if inside {
				return Some((s, origin.to_f64()));
			}
		}
		None
	}

	fn pointer_move(&mut self, x: i32, y: i32) {
		self.pointer = Point::from((x as f64, y as f64));
		let Some(ptr) = self.seat.get_pointer() else {
			return;
		};
		let focus = self.pointer_target(self.pointer);
		let event = MotionEvent {
			location: self.pointer,
			serial: SERIAL_COUNTER.next_serial(),
			time: self.now_ms(),
		};
		ptr.motion(self, focus, &event);
		ptr.frame(self);
	}

	fn pointer_button(&mut self, button: u8, pressed: bool) {
		let Some(ptr) = self.seat.get_pointer() else {
			return;
		};
		let code = EVDEV_BTN_LEFT
			+ match button {
				3 => 1,
				2 => 2,
				_ => 0,
			};
		let event = ButtonEvent {
			button: code,
			state: if pressed {
				ButtonState::Pressed
			} else {
				ButtonState::Released
			},
			serial: SERIAL_COUNTER.next_serial(),
			time: self.now_ms(),
		};
		ptr.button(self, &event);
		ptr.frame(self);
	}

	fn pointer_axis(&mut self, dx: i32, dy: i32) {
		let Some(ptr) = self.seat.get_pointer() else {
			return;
		};
		let mut frame = AxisFrame::new(self.now_ms()).source(AxisSource::Wheel);
		if dy != 0 {
			frame = frame
				.value(Axis::Vertical, -dy as f64 * 15.0)
				.v120(Axis::Vertical, -dy * 120);
		}
		if dx != 0 {
			frame = frame
				.value(Axis::Horizontal, dx as f64 * 15.0)
				.v120(Axis::Horizontal, dx * 120);
		}
		ptr.axis(self, frame);
		ptr.frame(self);
	}

	fn key(&mut self, keysym: u32, pressed: bool) {
		let Some((code, needs_shift)) = self.keys.find(keysym) else {
			eprintln!("tubeless-wm: no keycode for keysym {keysym:#x}");
			return;
		};
		let Some(kb) = self.seat.get_keyboard() else {
			return;
		};
		let state = if pressed {
			KeyState::Pressed
		} else {
			KeyState::Released
		};
		if needs_shift && pressed && !self.shift_held {
			self.feed_key(&kb, self.keys.shift, KeyState::Pressed);
			self.shift_held = true;
		}
		self.feed_key(&kb, Some(code), state);
		if needs_shift && !pressed && self.shift_held {
			self.feed_key(&kb, self.keys.shift, KeyState::Released);
			self.shift_held = false;
		}
	}

	fn feed_key(
		&mut self,
		kb: &smithay::input::keyboard::KeyboardHandle<State>,
		code: Option<smithay::input::keyboard::Keycode>,
		state: KeyState,
	) {
		let Some(code) = code else {
			return;
		};
		let serial = SERIAL_COUNTER.next_serial();
		let time = self.now_ms();
		kb.input::<(), _>(self, code, state, serial, time, |_, _, _| {
			FilterResult::Forward
		});
	}

	fn handle_window_message(&mut self, msg: FromWindow) {
		match msg {
			FromWindow::Resize { w, h } => self.resize(w as i32, h as i32),
			FromWindow::PointerMove { x, y } => self.pointer_move(x, y),
			FromWindow::PointerButton { button, pressed } => self.pointer_button(button, pressed),
			FromWindow::PointerAxis { dx, dy } => self.pointer_axis(dx, dy),
			FromWindow::Key { keysym, pressed } => self.key(keysym, pressed),
			FromWindow::Focus { focused } => self.set_focus(focused),
		}
	}
}

// Slides a popup back inside target when its positioner left it hanging
// over an edge (some toolkits ask for no constraint adjustment at all).
fn clamp_into(
	mut r: Rectangle<i32, Logical>,
	target: Rectangle<i32, Logical>,
) -> Rectangle<i32, Logical> {
	r.loc.x = r
		.loc
		.x
		.min(target.loc.x + target.size.w - r.size.w)
		.max(target.loc.x);
	r.loc.y = r
		.loc
		.y
		.min(target.loc.y + target.size.h - r.size.h)
		.max(target.loc.y);
	r
}

fn configure_toplevel(t: &ToplevelSurface, w: i32, h: i32, focused: bool) {
	t.with_pending_state(|s| {
		s.size = Some((w, h).into());
		s.states.set(xdg_toplevel::State::Maximized);
		if focused {
			s.states.set(xdg_toplevel::State::Activated);
		} else {
			s.states.unset(xdg_toplevel::State::Activated);
		}
		s.decoration_mode = Some(DecorationMode::ServerSide);
	});
	if t.is_initial_configure_sent() {
		t.send_configure();
	}
}

fn copy_buffer(buf: &wl_buffer::WlBuffer, img: &mut SurfaceImage) {
	let result = with_buffer_contents(buf, |ptr, len, data| {
		let bytes = unsafe { std::slice::from_raw_parts(ptr, len) };
		let (w, h) = (data.width, data.height);
		let mut out = Vec::with_capacity((w * h * 4) as usize);
		for y in 0..h as usize {
			let start = data.offset as usize + y * data.stride as usize;
			out.extend_from_slice(&bytes[start..start + w as usize * 4]);
		}
		let opaque =
			data.format == smithay::reexports::wayland_server::protocol::wl_shm::Format::Xrgb8888;
		if opaque {
			for px in out.chunks_exact_mut(4) {
				px[3] = 255;
			}
		}
		(w, h, out)
	});
	match result {
		Ok((w, h, data)) => *img = SurfaceImage { w, h, data },
		Err(e) => eprintln!("tubeless-wm: read shm buffer: {e:?}"),
	}
}

fn composite_tree(
	canvas: &mut Canvas,
	root: &WlSurface,
	origin: Point<i32, Logical>,
	rects: &mut HashMap<u32, Rect>,
) {
	with_surface_tree_downward(
		root,
		origin,
		|_, states, loc| {
			let mut loc = *loc;
			if states.role == Some(SUBSURFACE_ROLE) {
				loc += states
					.cached_state
					.get::<SubsurfaceCachedState>()
					.current()
					.location;
			}
			TraversalAction::DoChildren(loc)
		},
		|surface, states, loc| {
			let Some(cell) = states.data_map.get::<RefCell<SurfaceImage>>() else {
				return;
			};
			let img = cell.borrow();
			if img.w == 0 || img.h == 0 {
				return;
			}
			canvas.blit(&img, loc.x, loc.y);
			rects.insert(
				surface_key(surface),
				Rect {
					x: loc.x,
					y: loc.y,
					w: img.w,
					h: img.h,
				},
			);
		},
		|_, _, _| true,
	);
}

fn root_of(surface: &WlSurface) -> WlSurface {
	let mut cur = surface.clone();
	while let Some(parent) = get_parent(&cur) {
		cur = parent;
	}
	cur
}

impl CompositorHandler for State {
	fn compositor_state(&mut self) -> &mut CompositorState {
		&mut self.compositor
	}

	fn client_compositor_state<'a>(
		&self,
		client: &'a Client,
	) -> &'a smithay::wayland::compositor::CompositorClientState {
		&client.get_data::<AppClient>().unwrap().compositor
	}

	fn commit(&mut self, surface: &WlSurface) {
		// A sync subsurface's state only takes effect when its parent
		// commits, so its own commit has nothing to show yet.
		if is_sync_subsurface(surface) {
			return;
		}
		let root = root_of(surface);
		if let Some(t) = self.toplevels.iter().find(|t| t.wl_surface() == &root) {
			if !t.is_initial_configure_sent() {
				configure_toplevel(t, self.canvas.w, self.canvas.h, self.focused);
				t.send_configure();
			}
		}
		self.absorb_tree(surface);
		self.render();
	}

	fn destroyed(&mut self, surface: &WlSurface) {
		self.prev_rects.remove(&surface_key(surface));
		self.full_damage = true;
	}
}

impl BufferHandler for State {
	fn buffer_destroyed(&mut self, _: &wl_buffer::WlBuffer) {}
}

impl ShmHandler for State {
	fn shm_state(&self) -> &ShmState {
		&self.shm
	}
}

impl XdgShellHandler for State {
	fn xdg_shell_state(&mut self) -> &mut XdgShellState {
		&mut self.xdg_shell
	}

	fn new_toplevel(&mut self, surface: ToplevelSurface) {
		self.toplevels.push(surface);
		self.full_damage = true;
		if self.focused {
			self.set_focus(true);
		}
	}

	fn toplevel_destroyed(&mut self, surface: ToplevelSurface) {
		self.toplevels
			.retain(|t| t.wl_surface() != surface.wl_surface());
		self.full_damage = true;
		self.render();
	}

	fn new_popup(&mut self, surface: PopupSurface, positioner: PositionerState) {
		let parent_origin = surface
			.get_parent_surface()
			.and_then(|par| {
				self.roots()
					.into_iter()
					.find(|(s, _)| *s == par)
					.map(|(_, o)| o)
			})
			.unwrap_or_default();
		let target = Rectangle::new(
			Point::from((-parent_origin.x, -parent_origin.y)),
			(self.canvas.w, self.canvas.h).into(),
		);
		let geometry = clamp_into(positioner.get_unconstrained_geometry(target), target);
		surface.with_pending_state(|s| s.geometry = geometry);
		if let Err(e) = surface.send_configure() {
			eprintln!("tubeless-wm: configure popup: {e:?}");
		}
		self.popups.push(Popup {
			surface,
			rel: geometry.loc,
		});
		self.full_damage = true;
	}

	fn popup_destroyed(&mut self, surface: PopupSurface) {
		self.popups
			.retain(|p| p.surface.wl_surface() != surface.wl_surface());
		self.full_damage = true;
		self.render();
	}

	fn grab(&mut self, _: PopupSurface, _: wl_seat::WlSeat, _: Serial) {}

	fn reposition_request(
		&mut self,
		surface: PopupSurface,
		positioner: PositionerState,
		token: u32,
	) {
		let target = Rectangle::new(Point::from((0, 0)), (self.canvas.w, self.canvas.h).into());
		let geometry = clamp_into(positioner.get_unconstrained_geometry(target), target);
		surface.with_pending_state(|s| {
			s.geometry = geometry;
			s.positioner = positioner;
		});
		surface.send_repositioned(token);
		if let Err(e) = surface.send_configure() {
			eprintln!("tubeless-wm: configure repositioned popup: {e:?}");
		}
		for p in self
			.popups
			.iter_mut()
			.filter(|p| p.surface.wl_surface() == surface.wl_surface())
		{
			p.rel = geometry.loc;
		}
		self.full_damage = true;
	}
}

impl XdgDecorationHandler for State {
	fn new_decoration(&mut self, toplevel: ToplevelSurface) {
		toplevel.with_pending_state(|s| s.decoration_mode = Some(DecorationMode::ServerSide));
	}

	fn request_mode(&mut self, toplevel: ToplevelSurface, _: DecorationMode) {
		toplevel.with_pending_state(|s| s.decoration_mode = Some(DecorationMode::ServerSide));
		if toplevel.is_initial_configure_sent() {
			toplevel.send_configure();
		}
	}

	fn unset_mode(&mut self, toplevel: ToplevelSurface) {
		self.request_mode(toplevel, DecorationMode::ServerSide);
	}
}

impl SeatHandler for State {
	type KeyboardFocus = WlSurface;
	type PointerFocus = WlSurface;
	type TouchFocus = WlSurface;

	fn seat_state(&mut self) -> &mut SeatState<State> {
		&mut self.seat_state
	}
}

impl OutputHandler for State {}

impl SelectionHandler for State {
	type SelectionUserData = ();
}

impl DataDeviceHandler for State {
	fn data_device_state(&self) -> &DataDeviceState {
		&self.data_device
	}
}

impl ClientDndGrabHandler for State {}
impl ServerDndGrabHandler for State {}

delegate_compositor!(State);
delegate_shm!(State);
delegate_xdg_shell!(State);
delegate_xdg_decoration!(State);
delegate_seat!(State);
delegate_output!(State);
delegate_data_device!(State);

struct Args {
	fd: i32,
	width: i32,
	height: i32,
	app: Vec<String>,
}

fn parse_args() -> Result<Args, String> {
	let mut fd = None;
	let (mut width, mut height) = (800, 600);
	let mut it = std::env::args().skip(1);
	while let Some(a) = it.next() {
		match a.as_str() {
			"--fd" => fd = it.next().and_then(|v| v.parse().ok()),
			"--size" => {
				let v = it.next().unwrap_or_default();
				let (w, h) = v.split_once('x').ok_or("--size wants WxH")?;
				width = w.parse().map_err(|_| "bad width")?;
				height = h.parse().map_err(|_| "bad height")?;
			}
			"--" => break,
			other => return Err(format!("unknown argument {other}")),
		}
	}
	let app: Vec<String> = it.collect();
	if app.is_empty() {
		return Err("usage: tubeless-wm --fd N [--size WxH] -- app [args...]".into());
	}
	Ok(Args {
		fd: fd.ok_or("--fd is required")?,
		width,
		height,
		app,
	})
}

fn spawn_app(args: &Args, socket_name: &str) -> std::io::Result<std::process::Child> {
	let mut cmd = Command::new(&args.app[0]);
	cmd.args(&args.app[1..])
		.env("WAYLAND_DISPLAY", socket_name)
		.env("XDG_SESSION_TYPE", "wayland")
		.env_remove("DISPLAY");
	cmd.spawn()
}

fn kill_group() {
	use rustix::process::{getpgid, kill_process_group, Signal};
	match getpgid(None) {
		Ok(pgid) => {
			if let Err(e) = kill_process_group(pgid, Signal::TERM) {
				eprintln!("tubeless-wm: signal process group: {e}");
			}
		}
		Err(e) => eprintln!("tubeless-wm: look up process group: {e}"),
	}
}

fn run() -> Result<(), String> {
	let args = parse_args()?;
	let sock = unsafe { OwnedFd::from_raw_fd(args.fd) };
	let window_sock = rustix::io::dup(&sock).map_err(|e| format!("dup socket: {e}"))?;

	let mut event_loop: EventLoop<State> = EventLoop::try_new().map_err(|e| e.to_string())?;
	let display: Display<State> = Display::new().map_err(|e| e.to_string())?;
	let dh = display.handle();

	let output = Output::new(
		"tubeless-0".into(),
		PhysicalProperties {
			size: (0, 0).into(),
			subpixel: Subpixel::Unknown,
			make: "tubeless".into(),
			model: "wm".into(),
		},
	);
	let mode = Mode {
		size: (args.width, args.height).into(),
		refresh: 60_000,
	};
	output.change_current_state(Some(mode), None, None, None);
	output.set_preferred(mode);
	output.create_global::<State>(&dh);

	let mut seat_state = SeatState::new();
	let mut seat = seat_state.new_wl_seat(&dh, "seat0");
	seat.add_keyboard(XkbConfig::default(), 600, 25)
		.map_err(|e| format!("keyboard: {e:?}"))?;
	seat.add_pointer();

	let mut state = State {
		dh: dh.clone(),
		compositor: CompositorState::new::<State>(&dh),
		xdg_shell: XdgShellState::new::<State>(&dh),
		_decoration: XdgDecorationState::new::<State>(&dh),
		shm: ShmState::new::<State>(&dh, vec![]),
		seat_state,
		seat,
		data_device: DataDeviceState::new::<State>(&dh),
		output,
		start: Instant::now(),
		signal: event_loop.get_signal(),
		sock: window_sock,
		canvas: Canvas::new(args.width, args.height),
		shm_out: None,
		keys: KeyLookup::new().ok_or("compile xkb keymap")?,
		toplevels: Vec::new(),
		popups: Vec::new(),
		callbacks: Vec::new(),
		dirty: Vec::new(),
		prev_rects: HashMap::new(),
		full_damage: true,
		shift_held: false,
		focused: false,
		pointer: Point::from((0.0, 0.0)),
	};

	let listener = ListeningSocket::bind_auto("tubeless-wm", 1..64).map_err(|e| e.to_string())?;
	let socket_name = listener
		.socket_name()
		.and_then(|n| n.to_str())
		.ok_or("socket name is not utf-8")?
		.to_string();

	let handle = event_loop.handle();
	let mut client_dh = dh.clone();
	handle
		.insert_source(
			Generic::new(listener, Interest::READ, IoMode::Level),
			move |_, l, _| {
				loop {
					match l.accept() {
						Ok(Some(stream)) => {
							let data = Arc::new(AppClient {
								compositor: CompositorClientState::default(),
							});
							if let Err(e) = client_dh.insert_client(stream, data) {
								eprintln!("tubeless-wm: insert client: {e}");
							}
						}
						Ok(None) => break,
						Err(e) => {
							eprintln!("tubeless-wm: accept: {e}");
							break;
						}
					}
				}
				Ok(PostAction::Continue)
			},
		)
		.map_err(|e| e.to_string())?;

	handle
		.insert_source(
			Generic::new(display, Interest::READ, IoMode::Level),
			|_, display, st| {
				// SAFETY: the display is never dropped while the loop runs.
				if let Err(e) = unsafe { display.get_mut().dispatch_clients(st) } {
					eprintln!("tubeless-wm: dispatch clients: {e}");
				}
				Ok(PostAction::Continue)
			},
		)
		.map_err(|e| e.to_string())?;

	handle
		.insert_source(
			Generic::new(sock, Interest::READ, IoMode::Level),
			|_, sock, st| loop {
				match proto::recv(&*sock) {
					Ok(Recv::Message(m)) => st.handle_window_message(m),
					Ok(Recv::Unknown) => eprintln!("tubeless-wm: unknown message from window"),
					Ok(Recv::Drained) => return Ok(PostAction::Continue),
					Err(e) => {
						eprintln!("tubeless-wm: window socket closed: {e}");
						kill_group();
						st.signal.stop();
						return Ok(PostAction::Remove);
					}
				}
			},
		)
		.map_err(|e| e.to_string())?;

	handle
		.insert_source(Timer::from_duration(FRAME_INTERVAL), |_, _, st| {
			let now = st.now_ms();
			for cb in st.callbacks.drain(..) {
				cb.done(now);
			}
			TimeoutAction::ToDuration(FRAME_INTERVAL)
		})
		.map_err(|e| e.to_string())?;

	let (exit_tx, exit_rx) = channel::<i32>();
	handle
		.insert_source(exit_rx, |event, _, st| {
			if let ChannelEvent::Msg(code) = event {
				st.notify(&ToWindow::Exited { code });
				st.signal.stop();
			}
		})
		.map_err(|e| e.to_string())?;

	let mut child =
		spawn_app(&args, &socket_name).map_err(|e| format!("start {}: {e}", args.app[0]))?;
	std::thread::spawn(move || {
		let code = match child.wait() {
			Ok(status) => status.code().unwrap_or(-1),
			Err(e) => {
				eprintln!("tubeless-wm: wait for app: {e}");
				-1
			}
		};
		if exit_tx.send(code).is_err() {
			eprintln!("tubeless-wm: loop gone before app exit was reported");
		}
	});

	state.notify(&ToWindow::Hello);
	event_loop
		.run(None, &mut state, |st| {
			if let Err(e) = st.dh.flush_clients() {
				eprintln!("tubeless-wm: flush clients: {e}");
			}
		})
		.map_err(|e| e.to_string())
}

fn main() {
	if let Err(e) = run() {
		eprintln!("tubeless-wm: {e}");
		std::process::exit(1);
	}
}
