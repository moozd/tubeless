// demo: a WebGL shader in the terminal's colors, plus a bridge check.
export {};

const t = tubeless;
const gl = t.canvas.getContext('webgl2')!;

const panel = document.createElement('pre');
panel.style.cssText = 'position:fixed;top:0;left:0;margin:0;padding:1ch;background:var(--tb-bg)';
document.body.append(panel);

const hex = (h: string) => [1, 3, 5].map(i => parseInt(h.slice(i, i + 2), 16) / 255);

const prog = gl.createProgram()!;
for (const [type, src] of [
	[gl.VERTEX_SHADER, `#version 300 es
in vec2 p; void main() { gl_Position = vec4(p, 0, 1); }`],
	[gl.FRAGMENT_SHADER, `#version 300 es
precision highp float; uniform float t; uniform vec2 r; uniform vec3 a, b; out vec4 o;
void main() { vec2 u = gl_FragCoord.xy / r;
  float w = 0.5 + 0.5 * sin(t + u.x * 6.0 + sin(u.y * 4.0 + t));
  o = vec4(mix(a, b, w * 0.35), 1); }`],
] as const) {
	const s = gl.createShader(type)!;
	gl.shaderSource(s, src);
	gl.compileShader(s);
	gl.attachShader(prog, s);
}
gl.linkProgram(prog);
gl.useProgram(prog);
gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
gl.enableVertexAttribArray(0);
gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
const u = (n: string) => gl.getUniformLocation(prog, n);

let frames = 0, fps = 0, last = performance.now();
let fileText = '(no file arg)';
let lastKey = '(none)';
if (t.args[0]) {
	t.readText(t.args[0]).then(s => fileText = s.trim()).catch(e => fileText = String(e));
}

function draw(now: number) {
	gl.viewport(0, 0, t.canvas.width, t.canvas.height);
	gl.uniform1f(u('t'), now / 1000);
	gl.uniform2f(u('r'), t.canvas.width, t.canvas.height);
	gl.uniform3fv(u('a'), hex(t.theme.bg));
	gl.uniform3fv(u('b'), hex(t.theme.palette[4]));
	gl.drawArrays(gl.TRIANGLES, 0, 3);
	frames++;
	if (now - last > 1000) { fps = frames; frames = 0; last = now; }
	panel.textContent = [
		`demo: ${fps} fps, ${innerWidth}x${innerHeight} css px, dpr ${devicePixelRatio}`,
		`args: ${JSON.stringify(t.args)}  cwd: ${t.cwd}`,
		`font: ${t.theme.font.family} ${t.theme.font.size}px  fg ${t.theme.fg} bg ${t.theme.bg}`,
		`file: ${fileText}`,
		`last key: ${lastKey}`,
		`press q to quit with output`,
	].join('\n');
	requestAnimationFrame(draw);
}
requestAnimationFrame(draw);

addEventListener('keydown', e => {
	lastKey = `${e.key} (${e.code})`;
	if (e.key === 'q') t.exit(0, `demo closed after ${Math.round(performance.now() / 1000)}s\n`);
});
