// markdown: a markdown viewer in a tubeless pane.
//
//   tubeless exec markdown <file.md>
//
// GitHub-flavored markdown with highlighted code, in the terminal's
// font and colors. The page follows the file: save it in your editor
// and the pane shows the change within a second. Obsidian notes work:
// front matter is hidden, [[links]] and ![[images]] resolve.
//
// Keys: j/k scroll, d/u half page, space/b full page, g/G top/bottom,
// / find, n/N next/previous, Backspace back after a link, q quit
// (prints the file shown). Click a link to a .md file to open it.
import { Marked } from 'marked';
import { markedHighlight } from 'marked-highlight';
import hljs from 'highlight.js/lib/common';
import DOMPurify from 'dompurify';
import './style.css';

declare global {
	interface Window {
		// Chromium's own find in page; not in the DOM typings.
		find(text: string, caseSensitive?: boolean, backwards?: boolean, wrap?: boolean): boolean;
	}
}

const reloadEvery = 1000;

// An <img> decodes a blob by sniffing, except SVG: that needs its type.
const imageTypes: Record<string, string> = {
	svg: 'image/svg+xml', png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg',
	gif: 'image/gif', webp: 'image/webp', avif: 'image/avif',
};

const marked = new Marked(
	markedHighlight({
		emptyLangClass: 'hljs',
		langPrefix: 'hljs language-',
		highlight(code, lang) {
			const language = hljs.getLanguage(lang) ? lang : 'plaintext';
			return hljs.highlight(code, { language }).value;
		},
	}),
	{ gfm: true },
);

const scroller = el('div', 'scroller');
const article = el('article', 'markdown');
const status = el('div', 'status');
const finder = document.createElement('input');
finder.className = 'finder';
scroller.append(article);
document.body.append(scroller, status);

let file = tubeless.args.find((a) => !a.startsWith('-')) ?? '';
let shown = '';
let query = '';
let message = '';
const history: { file: string; top: number }[] = [];
const imageUrls: string[] = [];

function el(tag: string, className: string): HTMLDivElement {
	const node = document.createElement(tag) as HTMLDivElement;
	node.className = className;
	return node;
}

function dirname(path: string): string {
	const at = path.lastIndexOf('/');
	return at < 0 ? '.' : path.slice(0, at) || '/';
}

// resolve is a path relative to the file shown, as the bridge wants it.
function resolve(target: string): string {
	const clean = decodeURI(target.split('#')[0]);
	return clean.startsWith('/') ? clean : `${dirname(file)}/${clean}`;
}

function isExternal(href: string): boolean {
	return /^[a-z][a-z0-9+.-]*:/i.test(href);
}

// obsidianToMarkdown turns front matter and wiki links into plain
// markdown: ![[a.png]] is an image, [[note|text]] a link to note.md.
function obsidianToMarkdown(text: string): string {
	return text
		.replace(/^---\n[\s\S]*?\n---\n/, '')
		.replace(/!\[\[([^\]|]+)(?:\|[^\]]*)?\]\]/g, (_, target) => `![](<${target}>)`)
		.replace(/\[\[([^\]|#]+)(#[^\]|]*)?(?:\|([^\]]+))?\]\]/g, (_, target, anchor, alias) =>
			`[${alias ?? target}](<${/\.\w+$/.test(target) ? target : target + '.md'}${anchor ?? ''}>)`);
}

function slug(text: string): string {
	return text.toLowerCase().trim().replace(/[^\p{L}\p{N}\s-]/gu, '').replace(/\s+/g, '-');
}

function render(text: string) {
	const html = marked.parse(obsidianToMarkdown(text), { async: false }) as string;
	article.innerHTML = DOMPurify.sanitize(html);
	article.querySelectorAll('h1, h2, h3, h4, h5, h6').forEach((h) => (h.id = slug(h.textContent ?? '')));
	loadImages();
	updateStatus();
}

// loadImages reads local images through the bridge: the page cannot
// load files itself.
function loadImages() {
	imageUrls.splice(0).forEach((u) => URL.revokeObjectURL(u));
	article.querySelectorAll('img').forEach(async (img) => {
		const src = img.getAttribute('src') ?? '';
		if (!src || isExternal(src)) return;
		try {
			const data = await tubeless.readFile(resolve(src));
			const ext = src.slice(src.lastIndexOf('.') + 1).toLowerCase();
			const url = URL.createObjectURL(new Blob([data], { type: imageTypes[ext] ?? '' }));
			imageUrls.push(url);
			img.src = url;
		} catch {
			img.alt = `[missing image: ${src}]`;
		}
	});
}

async function open(next: string, top = 0, anchor = '') {
	file = next;
	shown = '';
	await reload();
	scroller.scrollTop = top;
	if (anchor) scrollToHeading(anchor);
}

// scrollToHeading shows the heading an anchor names: "#Part two" and
// "#part-two" both find the heading "Part two".
function scrollToHeading(anchor: string) {
	document.getElementById(slug(decodeURI(anchor)))?.scrollIntoView();
}

// reload renders the file again when it changed on disk. A scroll
// position survives the change.
async function reload() {
	try {
		const text = await tubeless.readText(file);
		if (text === shown) return;
		const top = scroller.scrollTop;
		shown = text;
		message = '';
		render(text);
		scroller.scrollTop = top;
	} catch (err) {
		message = `cannot read: ${err instanceof Error ? err.message : err}`;
		updateStatus();
	}
}

function updateStatus() {
	const max = scroller.scrollHeight - scroller.clientHeight;
	const pct = max > 0 ? Math.round((scroller.scrollTop / max) * 100) : 100;
	status.textContent = ` ${file}  ${pct}%  ${message}`;
}

article.addEventListener('click', (e) => {
	const link = (e.target as HTMLElement).closest('a');
	if (!link) return;
	e.preventDefault();
	followLink(link.getAttribute('href') ?? '');
});

function followLink(href: string) {
	if (isExternal(href)) {
		message = `external link: ${href}`;
		updateStatus();
		return;
	}
	const [path, anchor = ''] = href.split('#');
	if (!path) {
		scrollToHeading(anchor);
		return;
	}
	history.push({ file, top: scroller.scrollTop });
	open(resolve(path), 0, anchor);
}

function back() {
	const last = history.pop();
	if (last) open(last.file, last.top);
}

function cellHeight(): number {
	return tubeless.theme.cell.height || 20;
}

function findNext(backwards: boolean) {
	if (!query) return;
	const found = window.find(query, false, backwards, true);
	message = found ? '' : `no match: ${query}`;
	updateStatus();
}

const keys: Record<string, () => void> = {
	j: () => scroller.scrollBy(0, 3 * cellHeight()),
	k: () => scroller.scrollBy(0, -3 * cellHeight()),
	ArrowDown: () => scroller.scrollBy(0, 3 * cellHeight()),
	ArrowUp: () => scroller.scrollBy(0, -3 * cellHeight()),
	d: () => scroller.scrollBy(0, innerHeight / 2),
	u: () => scroller.scrollBy(0, -innerHeight / 2),
	' ': () => scroller.scrollBy(0, innerHeight - 2 * cellHeight()),
	b: () => scroller.scrollBy(0, -(innerHeight - 2 * cellHeight())),
	g: () => (scroller.scrollTop = 0),
	G: () => (scroller.scrollTop = scroller.scrollHeight),
	'/': () => openFinder(),
	n: () => findNext(false),
	N: () => findNext(true),
	Backspace: back,
	q: () => tubeless.exit(0, `${file}\n`),
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

finder.addEventListener('keydown', (e) => {
	if (e.key !== 'Enter' && e.key !== 'Escape') return;
	e.preventDefault();
	if (e.key === 'Enter') query = finder.value;
	finder.blur();
	updateStatus();
	if (e.key === 'Enter') findNext(false);
});

scroller.addEventListener('scroll', updateStatus);

if (!file) {
	tubeless.exit(2, 'usage: tubeless exec markdown <file.md>\n');
} else {
	open(file);
	setInterval(reload, reloadEvery);
}
