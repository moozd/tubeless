// pdf: a PDF reader in a tubeless pane.
//
//   tubeless exec pdf <file.pdf> [--page N] [--no-recolor]
//
// Keys: j/k scroll, d/u half page, space/b full page, g/G first/last
// page, +/- zoom, 0 fit width, / find text, n/N next/previous match,
// Esc clear the matches, q quit (prints file:page). Pages use the terminal's own fg/bg, the
// way the text around the pane looks; --no-recolor shows their own
// colors.
import * as pdfjs from 'pdfjs-dist';
import './style.css';

pdfjs.GlobalWorkerOptions.workerSrc = '/node_modules/pdfjs-dist/build/pdf.worker.min.mjs';

interface Options {
	file: string;
	page: number;
	isRecolor: boolean;
}

interface PageView {
	index: number;
	page: pdfjs.PDFPageProxy;
	box: HTMLDivElement;
	canvas: HTMLCanvasElement;
	text: HTMLDivElement;
	renderedScale: number;
	task?: pdfjs.RenderTask;
	layer?: pdfjs.TextLayer;
}

// Match is one occurrence of the search text: its page, and its rank
// among the matches on that page.
interface Match {
	page: number;
	rank: number;
}

const pageGap = 8;
const zoomStep = 1.2;

function parseArgs(args: string[]): Options {
	const opts: Options = { file: '', page: 1, isRecolor: true };
	for (let i = 0; i < args.length; i++) {
		switch (args[i]) {
		case '--page':
			opts.page = Number(args[++i]) || 1;
			break;
		case '--no-recolor':
			opts.isRecolor = false;
			break;
		default:
			opts.file = args[i];
		}
	}
	return opts;
}

const opts = parseArgs(tubeless.args);
const scroller = el('div', 'scroller');
const status = el('div', 'status');
const finder = document.createElement('input');
finder.className = 'finder';
document.body.append(scroller, status);

let pages: PageView[] = [];
let zoom = 0; // 0 fits the pane width
let query = '';
let matches: Match[] = [];
let matchAt = -1;
let isScrollPending = false;
let message = '';
const texts = new Map<number, string[]>();

function el(tag: string, className: string): HTMLDivElement {
	const node = document.createElement(tag) as HTMLDivElement;
	node.className = className;
	return node;
}

function quit(code: number, output: string) {
	tubeless.exit(code, output);
}

async function open() {
	if (!opts.file) {
		quit(2, 'usage: tubeless exec pdf <file.pdf> [--page N] [--no-recolor]\n');
		return;
	}
	const data = await tubeless.readFile(opts.file);
	const doc = await pdfjs.getDocument({
		data,
		standardFontDataUrl: '/node_modules/pdfjs-dist/standard_fonts/',
		cMapUrl: '/node_modules/pdfjs-dist/cmaps/',
		cMapPacked: true,
	}).promise;
	for (let i = 1; i <= doc.numPages; i++) {
		pages.push(newPageView(i, await doc.getPage(i)));
	}
	layout();
	goToPage(opts.page);
}

function newPageView(index: number, page: pdfjs.PDFPageProxy): PageView {
	const box = el('div', 'page');
	const canvas = document.createElement('canvas');
	const text = el('div', 'textLayer');
	box.append(canvas, text);
	box.dataset.index = String(index);
	scroller.append(box);
	observer.observe(box);
	return { index, page, box, canvas, text, renderedScale: 0 };
}

// scaleFor is the CSS scale of a page at the current zoom.
function scaleFor(view: PageView): number {
	if (zoom > 0) return zoom;
	const width = view.page.getViewport({ scale: 1 }).width;
	return (scroller.clientWidth - 2 * pageGap) / width;
}

function layout() {
	for (const view of pages) {
		const vp = view.page.getViewport({ scale: scaleFor(view) });
		view.box.style.width = `${Math.floor(vp.width)}px`;
		view.box.style.height = `${Math.floor(vp.height)}px`;
	}
	pages.filter(isVisible).forEach(render);
	updateStatus();
}

function isVisible(view: PageView): boolean {
	const r = view.box.getBoundingClientRect();
	return r.bottom > 0 && r.top < innerHeight;
}

const observer = new IntersectionObserver((entries) => {
	for (const e of entries) {
		const view = pages[Number((e.target as HTMLElement).dataset.index) - 1];
		if (e.isIntersecting && view) render(view);
	}
}, { root: null, rootMargin: '100% 0px' });

async function render(view: PageView) {
	const scale = scaleFor(view);
	if (view.renderedScale === scale) return;
	view.renderedScale = scale;
	view.task?.cancel();
	const vp = view.page.getViewport({ scale });
	const dpr = devicePixelRatio;
	view.canvas.width = Math.floor(vp.width * dpr);
	view.canvas.height = Math.floor(vp.height * dpr);
	view.task = view.page.render({
		canvas: view.canvas,
		viewport: vp,
		transform: dpr === 1 ? undefined : [dpr, 0, 0, dpr, 0, 0],
		pageColors: opts.isRecolor ? { background: tubeless.theme.bg, foreground: tubeless.theme.fg } : undefined,
	});
	await view.task.promise.catch(ignoreCancel);
	await renderText(view, vp);
}

async function renderText(view: PageView, vp: pdfjs.PageViewport) {
	view.text.replaceChildren();
	view.text.style.setProperty('--total-scale-factor', String(vp.scale));
	const layer = new pdfjs.TextLayer({
		textContentSource: view.page.streamTextContent(),
		container: view.text,
		viewport: vp,
	});
	await layer.render().catch(ignoreCancel);
	view.layer = layer;
	highlight(view);
	if (isScrollPending && matches[matchAt]?.page === view.index) scrollToCurrent(view);
}

function ignoreCancel(err: unknown) {
	if (err instanceof Error && err.name === 'RenderingCancelledException') return;
	console.error(err);
}

function currentPage(): number {
	const mid = innerHeight / 3;
	const hit = pages.find((v) => v.box.getBoundingClientRect().bottom > mid);
	return hit ? hit.index : pages.length;
}

function goToPage(n: number) {
	const view = pages[Math.min(Math.max(n, 1), pages.length) - 1];
	if (view) scroller.scrollTop = view.box.offsetTop - pageGap;
	updateStatus();
}

function updateStatus() {
	const pct = Math.round(scaleFor(pages[0] ?? ({} as PageView)) * 100);
	const where = `${currentPage()}/${pages.length}`;
	status.textContent = ` ${opts.file}  ${where}  ${zoom ? pct + '%' : 'fit'}  ${message}`;
}

function setZoom(next: number) {
	const page = currentPage();
	zoom = next;
	layout();
	goToPage(page);
}

function cellHeight(): number {
	return tubeless.theme.cell.height || 20;
}

const keys: Record<string, () => void> = {
	j: () => scroller.scrollBy(0, 3 * cellHeight()),
	k: () => scroller.scrollBy(0, -3 * cellHeight()),
	ArrowDown: () => scroller.scrollBy(0, 3 * cellHeight()),
	ArrowUp: () => scroller.scrollBy(0, -3 * cellHeight()),
	d: () => scroller.scrollBy(0, innerHeight / 2),
	u: () => scroller.scrollBy(0, -innerHeight / 2),
	' ': () => scroller.scrollBy(0, innerHeight - cellHeight()),
	b: () => scroller.scrollBy(0, -(innerHeight - cellHeight())),
	PageDown: () => scroller.scrollBy(0, innerHeight - cellHeight()),
	PageUp: () => scroller.scrollBy(0, -(innerHeight - cellHeight())),
	g: () => goToPage(1),
	G: () => goToPage(pages.length),
	'+': () => setZoom(scaleFor(pages[0]) * zoomStep),
	'=': () => setZoom(scaleFor(pages[0]) * zoomStep),
	'-': () => setZoom(scaleFor(pages[0]) / zoomStep),
	'0': () => setZoom(0),
	'/': () => openFinder(),
	n: () => nextMatch(1),
	N: () => nextMatch(-1),
	Escape: () => clearFind(),
	q: () => quit(0, `${opts.file}:${currentPage()}\n`),
};

addEventListener('keydown', (e) => {
	if (document.activeElement === finder || e.ctrlKey || e.metaKey || e.altKey) return;
	const action = keys[e.key];
	if (!action) return;
	e.preventDefault();
	action();
});

function openFinder() {
	finder.value = '';
	status.replaceChildren('/', finder);
	finder.focus();
}

finder.addEventListener('keydown', async (e) => {
	if (e.key === 'Escape') {
		closeFinder();
		return;
	}
	if (e.key !== 'Enter') return;
	e.preventDefault();
	await find(finder.value);
	closeFinder();
});

function closeFinder() {
	finder.blur();
	updateStatus();
}

// pageItems is the page's text, one string per text item, lowercased.
// The text layer makes one span per item, in the same order.
async function pageItems(view: PageView): Promise<string[]> {
	const cached = texts.get(view.index);
	if (cached !== undefined) return cached;
	const content = await view.page.getTextContent();
	const items = content.items.filter((i) => 'str' in i).map((i) => (i as { str: string }).str.toLowerCase());
	texts.set(view.index, items);
	return items;
}

// countIn is how many times needle occurs in text.
function countIn(text: string, needle: string): number {
	let count = 0;
	for (let at = text.indexOf(needle); at >= 0; at = text.indexOf(needle, at + needle.length)) {
		count++;
	}
	return count;
}

async function find(text: string) {
	query = text.toLowerCase();
	matches = [];
	for (const view of pages) {
		const total = query ? (await pageItems(view)).reduce((n, s) => n + countIn(s, query), 0) : 0;
		for (let rank = 0; rank < total; rank++) matches.push({ page: view.index, rank });
	}
	matchAt = -1;
	message = matches.length || !query ? '' : `no match: ${text}`;
	pages.forEach(highlight);
	nextMatch(1);
}

function clearFind() {
	query = '';
	matches = [];
	matchAt = -1;
	message = '';
	pages.forEach(highlight);
	updateStatus();
}

// nextMatch moves to the next match after the current one, or the
// first match below the current page when none is chosen yet.
function nextMatch(step: number) {
	if (!matches.length) return;
	if (matchAt < 0) {
		const from = currentPage();
		const ahead = matches.findIndex((m) => m.page >= from);
		matchAt = step > 0 ? Math.max(ahead, 0) : (ahead > 0 ? ahead - 1 : matches.length - 1);
	} else {
		matchAt = (matchAt + step + matches.length) % matches.length;
	}
	message = `match ${matchAt + 1}/${matches.length}`;
	showMatch(matches[matchAt]);
}

// showMatch scrolls the match to the middle of the pane. A page that
// is not rendered yet scrolls into view first; its text layer then
// marks the match when it renders.
function showMatch(m: Match) {
	const view = pages[m.page - 1];
	pages.forEach(highlight);
	isScrollPending = !view.layer || view.renderedScale !== scaleFor(view);
	if (isScrollPending) goToPage(m.page);
	else scrollToCurrent(view);
	updateStatus();
}

function scrollToCurrent(view: PageView) {
	isScrollPending = false;
	view.text.querySelector('mark.current')?.scrollIntoView({ block: 'center' });
}

// highlight marks every match on a rendered page, and the current
// match in its own color.
function highlight(view: PageView) {
	if (!view.layer) return;
	const current = matches[matchAt];
	const currentRank = current?.page === view.index ? current.rank : -1;
	let rank = 0;
	view.layer.textDivs.forEach((div, i) => {
		const text = view.layer!.textContentItemsStr[i] ?? '';
		rank = markText(div, text, rank, currentRank);
	});
}

// markText rebuilds one text span with <mark> around each match. It
// returns the rank of the next match.
function markText(div: HTMLElement, text: string, rank: number, currentRank: number): number {
	if (!query || !text.toLowerCase().includes(query)) {
		if (div.childElementCount) div.textContent = text;
		return rank;
	}
	const parts: (string | HTMLElement)[] = [];
	const lower = text.toLowerCase();
	let from = 0;
	for (let at = lower.indexOf(query); at >= 0; at = lower.indexOf(query, from)) {
		const mark = document.createElement('mark');
		mark.textContent = text.slice(at, at + query.length);
		mark.classList.toggle('current', rank === currentRank);
		parts.push(text.slice(from, at), mark);
		from = at + query.length;
		rank++;
	}
	parts.push(text.slice(from));
	div.replaceChildren(...parts);
	return rank;
}

scroller.addEventListener('scroll', updateStatus);
tubeless.on('resize', layout);
tubeless.on('theme', () => {
	if (!opts.isRecolor) return;
	pages.forEach((v) => (v.renderedScale = 0));
	layout();
});

open().catch((err) => {
	status.textContent = ` ${opts.file}: ${err.message ?? err}`;
});
