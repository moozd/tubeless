// Maps an X11 keysym to the xkb keycode (and whether Shift is needed)
// that produces it on the compositor's keymap, so text arriving from
// the tmux pane can be replayed as real Wayland key events.

use std::collections::HashMap;

use smithay::input::keyboard::{xkb, Keycode};

pub struct KeyLookup {
	by_sym: HashMap<u32, (Keycode, bool)>,
	pub shift: Option<Keycode>,
}

impl KeyLookup {
	pub fn new() -> Option<Self> {
		let ctx = xkb::Context::new(xkb::CONTEXT_NO_FLAGS);
		let keymap =
			xkb::Keymap::new_from_names(&ctx, "", "", "", "", None, xkb::KEYMAP_COMPILE_NO_FLAGS)?;
		let mut by_sym = HashMap::new();
		for raw in keymap.min_keycode().raw()..=keymap.max_keycode().raw() {
			let code = Keycode::new(raw);
			for level in 0..2 {
				for sym in keymap.key_get_syms_by_level(code, 0, level) {
					by_sym.entry(sym.raw()).or_insert((code, level == 1));
				}
			}
		}
		let shift = by_sym.get(&0xffe1).map(|(code, _)| *code);
		Some(Self { by_sym, shift })
	}

	pub fn find(&self, keysym: u32) -> Option<(Keycode, bool)> {
		self.by_sym.get(&keysym).copied()
	}
}

#[cfg(test)]
mod tests {
	use super::*;

	#[test]
	fn finds_plain_shifted_and_modifier_keysyms() {
		let keys = KeyLookup::new().expect("keymap compiles");
		let (_, shifted) = keys.find('a' as u32).expect("a");
		assert!(!shifted);
		let (_, shifted) = keys.find('A' as u32).expect("A");
		assert!(shifted);
		assert!(keys.find(0xff0d).is_some(), "Return");
		assert!(keys.shift.is_some(), "Shift_L");
	}
}
