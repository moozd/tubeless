package applet

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// shellPage is the built-in page around an applet's main.ts: the
// tubeless runtime and stylesheet, one full-pane canvas, and the
// applet's bundle.
func shellPage(inst Instance, hasCSS bool) string {
	title := inst.Manifest.Title
	if title == "" {
		title = inst.Name
	}
	css := ""
	if hasCSS {
		css = `<link rel="stylesheet" href="/main.css">`
	}
	return `<!doctype html>
<html><head><meta charset="utf-8">
<title>` + html.EscapeString(title) + `</title>
<link rel="stylesheet" href="/tubeless.css">
` + css + `
<script src="/tubeless.js"></script>
</head><body>
<canvas id="tubeless"></canvas>
<script type="module" src="/main.js"></script>
</body></html>
`
}

// baseCSS makes plain DOM look like the terminal: its colors, its font,
// flat controls, thin scrollbars. Applet CSS loads after it and wins.
const baseCSS = `
html,body{margin:0;height:100%;overflow:hidden;background:var(--tb-bg);color:var(--tb-fg);
  font-family:var(--tb-font);font-size:var(--tb-font-size);line-height:var(--tb-cell-height);
  -webkit-font-smoothing:antialiased}
#tubeless{position:fixed;inset:0;width:100vw;height:100vh;display:block;z-index:-1}
pre,code,kbd,samp,tt{font-family:inherit;font-size:inherit}
::selection{background:var(--tb-selection-bg);color:var(--tb-selection-fg)}
*{caret-color:var(--tb-cursor);accent-color:var(--tb-ansi-4);scrollbar-width:thin;
  scrollbar-color:var(--tb-ansi-8) transparent}
:focus-visible{outline:1px solid var(--tb-ansi-4);outline-offset:0}
button,input,select,textarea{font:inherit;color:var(--tb-fg);background:var(--tb-bg);
  border:1px solid var(--tb-ansi-8);border-radius:0;padding:0 1ch}
button:hover{background:var(--tb-ansi-8)}
a{color:var(--tb-ansi-4)}
`

// runtimeJS defines window.tubeless. __INIT__ is replaced with the
// instance's name, args, cwd and theme.
const runtimeJS = `(() => {
const init = __INIT__;
const listeners = {resize: [], focus: [], blur: [], theme: []};
const call = async (name, opts) => {
	const r = await fetch('/api/' + name, opts);
	if (!r.ok) throw new Error('tubeless.' + name + ': ' + await r.text());
	return r;
};
const themeStyle = document.createElement('style');
const fit = () => {
	const c = document.getElementById('tubeless');
	if (!c) return;
	c.width = Math.round(innerWidth * devicePixelRatio);
	c.height = Math.round(innerHeight * devicePixelRatio);
};
const tubeless = {
	name: init.name,
	args: init.args,
	cwd: init.cwd,
	theme: init.theme,
	get canvas() { return document.getElementById('tubeless'); },
	async readFile(path) {
		const r = await call('read?path=' + encodeURIComponent(path));
		return new Uint8Array(await r.arrayBuffer());
	},
	async readText(path) {
		return new TextDecoder().decode(await tubeless.readFile(path));
	},
	async writeFile(path, data) {
		await call('write?path=' + encodeURIComponent(path), {method: 'POST', body: data});
	},
	exit(code = 0, output = '') {
		fetch('/api/exit', {method: 'POST', body: JSON.stringify({code, output: String(output)})});
	},
	on(event, fn) {
		if (!listeners[event]) throw new Error('tubeless.on: no event ' + event);
		listeners[event].push(fn);
	},
	_setTheme(theme) {
		tubeless.theme = theme;
		themeStyle.textContent = theme.css;
		listeners.theme.forEach(fn => fn(theme));
	},
};
addEventListener('resize', () => { fit(); listeners.resize.forEach(fn => fn()); });
addEventListener('focus', () => listeners.focus.forEach(fn => fn()));
addEventListener('blur', () => listeners.blur.forEach(fn => fn()));
document.addEventListener('DOMContentLoaded', () => { document.head.append(themeStyle); fit(); });
window.tubeless = tubeless;
})();
`

func runtimeResponse(inst Instance, theme Theme) Response {
	init := struct {
		Name  string   `json:"name"`
		Args  []string `json:"args"`
		Cwd   string   `json:"cwd"`
		Theme Theme    `json:"theme"`
	}{inst.Name, inst.Args, inst.Cwd, theme}
	if init.Args == nil {
		init.Args = []string{}
	}
	data, err := json.Marshal(init)
	if err != nil {
		return textResponse(500, fmt.Sprintf("encode applet init: %v", err))
	}
	js := strings.Replace(runtimeJS, "__INIT__", string(data), 1)
	return Response{Status: 200, MIME: "text/javascript", Body: []byte(js)}
}

// errorPage shows why an applet could not load, in the pane itself.
func errorPage(name string, err error) Response {
	body := `<!doctype html><html><head><meta charset="utf-8">
<link rel="stylesheet" href="/tubeless.css"></head><body style="padding:1ch;white-space:pre-wrap">` +
		html.EscapeString("applet "+name+": "+err.Error()) + `</body></html>`
	return Response{Status: 200, MIME: "text/html", Body: []byte(body)}
}
