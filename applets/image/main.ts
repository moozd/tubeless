// image: an image viewer in a tubeless pane.
//
//   tubeless exec image <file>...
//
// Chromium decodes the image: PNG, JPEG, GIF (animated too), WebP,
// AVIF, SVG, BMP and ICO. Keys: n/p or arrows next/previous file,
// g/G first/last, +/- zoom, 0 fit, 1 actual size, h/j/k/l pan, r/R
// rotate, q quit (prints the file shown). The wheel zooms at the
// pointer; drag to pan.
import './style.css';

const zoomStep = 1.25;
const minScale = 0.02;
const maxScale = 64;

const mimeTypes: Record<string, string> = {
	png: 'image/png', apng: 'image/apng', jpg: 'image/jpeg', jpeg: 'image/jpeg',
	gif: 'image/gif', webp: 'image/webp', avif: 'image/avif',
	svg: 'image/svg+xml', bmp: 'image/bmp', ico: 'image/x-icon',
};

interface View {
	zoom: number; // 0 fits the pane
	turns: number; // quarter turns clockwise
	x: number; // pan offset from the pane center, CSS px
	y: number;
}

const files = tubeless.args.filter((a) => !a.startsWith('--'));
const stage = el('div', 'stage');
const img = document.createElement('img');
const status = el('div', 'status');
stage.append(img);
document.body.append(stage, status);

let index = 0;
let view: View = { zoom: 0, turns: 0, x: 0, y: 0 };
let objectUrl = '';
let message = '';

function el(tag: string, className: string): HTMLDivElement {
	const node = document.createElement(tag) as HTMLDivElement;
	node.className = className;
	return node;
}

function mimeOf(file: string): string {
	const ext = file.slice(file.lastIndexOf('.') + 1).toLowerCase();
	return mimeTypes[ext] ?? 'application/octet-stream';
}

async function show(i: number) {
	index = Math.min(Math.max(i, 0), files.length - 1);
	view = { zoom: 0, turns: 0, x: 0, y: 0 };
	message = '';
	try {
		const data = await tubeless.readFile(files[index]);
		URL.revokeObjectURL(objectUrl);
		objectUrl = URL.createObjectURL(new Blob([data], { type: mimeOf(files[index]) }));
		img.src = objectUrl;
		await img.decode();
	} catch (err) {
		message = `cannot show: ${err instanceof Error ? err.message : err}`;
	}
	render();
}

// size is the image's box after rotation, at scale 1.
function size(): { w: number; h: number } {
	const w = img.naturalWidth || 1;
	const h = img.naturalHeight || 1;
	return view.turns % 2 ? { w: h, h: w } : { w, h };
}

// fitScale shrinks a big image to the pane; a small one stays 1:1.
function fitScale(): number {
	const { w, h } = size();
	return Math.min(stage.clientWidth / w, stage.clientHeight / h, 1);
}

function scale(): number {
	return view.zoom || fitScale();
}

function render() {
	const s = scale();
	const cx = stage.clientWidth / 2 + view.x;
	const cy = stage.clientHeight / 2 + view.y;
	img.style.transform = `translate(${cx}px, ${cy}px) rotate(${view.turns * 90}deg) ` +
		`scale(${s}) translate(${-img.naturalWidth / 2}px, ${-img.naturalHeight / 2}px)`;
	img.classList.toggle('pixelated', s >= 2);
	updateStatus(s);
}

function updateStatus(s: number) {
	const name = files[index] ?? '';
	const dims = img.naturalWidth ? `${img.naturalWidth}x${img.naturalHeight}` : '';
	const zoom = view.zoom ? `${Math.round(s * 100)}%` : `fit ${Math.round(s * 100)}%`;
	const count = files.length > 1 ? `${index + 1}/${files.length}` : '';
	status.textContent = ` ${name}  ${dims}  ${zoom}  ${count}  ${message}`;
}

// zoomAt sets the scale and keeps the image point under (px, py)
// where it is.
function zoomAt(next: number, px = stage.clientWidth / 2, py = stage.clientHeight / 2) {
	const s = scale();
	const clamped = Math.min(Math.max(next, minScale), maxScale);
	const ox = px - stage.clientWidth / 2;
	const oy = py - stage.clientHeight / 2;
	view.x = ox - (ox - view.x) * (clamped / s);
	view.y = oy - (oy - view.y) * (clamped / s);
	view.zoom = clamped;
	render();
}

function pan(dx: number, dy: number) {
	view.x += dx;
	view.y += dy;
	render();
}

function rotate(turns: number) {
	view.turns = (view.turns + turns + 4) % 4;
	render();
}

function fit() {
	view = { ...view, zoom: 0, x: 0, y: 0 };
	render();
}

function step(): number {
	return 3 * (tubeless.theme.cell.height || 20);
}

const keys: Record<string, () => void> = {
	n: () => show(index + 1),
	p: () => show(index - 1),
	ArrowRight: () => show(index + 1),
	ArrowLeft: () => show(index - 1),
	' ': () => show(index + 1),
	Backspace: () => show(index - 1),
	g: () => show(0),
	G: () => show(files.length - 1),
	'+': () => zoomAt(scale() * zoomStep),
	'=': () => zoomAt(scale() * zoomStep),
	'-': () => zoomAt(scale() / zoomStep),
	'0': fit,
	'1': () => zoomAt(1),
	h: () => pan(step(), 0),
	l: () => pan(-step(), 0),
	k: () => pan(0, step()),
	j: () => pan(0, -step()),
	r: () => rotate(1),
	R: () => rotate(-1),
	q: () => tubeless.exit(0, `${files[index]}\n`),
};

addEventListener('keydown', (e) => {
	if (e.ctrlKey || e.metaKey || e.altKey) return;
	const action = keys[e.key];
	if (!action) return;
	e.preventDefault();
	action();
});

stage.addEventListener('wheel', (e) => {
	e.preventDefault();
	zoomAt(scale() * (e.deltaY < 0 ? zoomStep : 1 / zoomStep), e.offsetX, e.offsetY);
}, { passive: false });

let drag: { x: number; y: number } | null = null;

stage.addEventListener('pointerdown', (e) => {
	drag = { x: e.clientX, y: e.clientY };
	stage.setPointerCapture(e.pointerId);
});

stage.addEventListener('pointermove', (e) => {
	if (!drag) return;
	pan(e.clientX - drag.x, e.clientY - drag.y);
	drag = { x: e.clientX, y: e.clientY };
});

stage.addEventListener('pointerup', () => (drag = null));
tubeless.on('resize', render);

if (files.length === 0) {
	tubeless.exit(2, 'usage: tubeless exec image <file>...\n');
} else {
	show(0);
}
