#version 330 core

out vec4 fragColor;

uniform vec2 uRes;
uniform vec4 uPanel;      // x, y, w, h (px)
uniform float uRadius;    // panel corner radius
uniform vec4 uPanelColor;
uniform vec4 uBorderColor;
uniform float uBorderWidth;
uniform vec4 uBar;        // x, y, w, h of the bar track (px)
uniform float uBarRadius;
uniform vec4 uBarTrack;
uniform vec4 uBarFill;
uniform float uFrac;      // 0..1 fill fraction
uniform float uDim;       // backdrop dim alpha

float sdRoundBox(vec2 p, vec2 b, float r) {
	vec2 q = abs(p) - b + r;
	return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;
}

void main() {
	vec2 px = vec2(gl_FragCoord.x, uRes.y - gl_FragCoord.y);

	// Backdrop dim — straight-alpha black over the scene already in the
	// framebuffer, so the frozen frame shows through but reads as busy.
	vec4 col = vec4(0.0, 0.0, 0.0, uDim);

	// Panel: a hairline border ring around an opaque rounded-rect fill. The
	// outer rect grows radius by the same amount as its half-size, so the
	// ring is uniform width all the way around the corners.
	vec2 pc = uPanel.xy + uPanel.zw * 0.5;
	vec2 hp = uPanel.zw * 0.5;
	float cOuter = 1.0 - smoothstep(-1.0, 1.0, sdRoundBox(px - pc, hp + uBorderWidth, uRadius + uBorderWidth));
	float cInner = 1.0 - smoothstep(-1.0, 1.0, sdRoundBox(px - pc, hp, uRadius));
	float cBorder = cOuter - cInner;
	col.rgb = mix(col.rgb, uBorderColor.rgb, cBorder);
	col.a   = mix(col.a,   uBorderColor.a,   cBorder);
	col.rgb = mix(col.rgb, uPanelColor.rgb, cInner);
	col.a   = mix(col.a,   uPanelColor.a,   cInner);

	// Bar track.
	vec2 bc = uBar.xy + uBar.zw * 0.5;
	float cb = 1.0 - smoothstep(-1.0, 1.0, sdRoundBox(px - bc, uBar.zw * 0.5, uBarRadius));
	col.rgb = mix(col.rgb, uBarTrack.rgb, cb);
	col.a   = mix(col.a,   uBarTrack.a,   cb);

	// Bar fill, left-aligned.
	if (uFrac > 0.001) {
		float fw = uBar.z * uFrac;
		vec2 fc = vec2(uBar.x + fw * 0.5, uBar.y + uBar.w * 0.5);
		float cf = 1.0 - smoothstep(-1.0, 1.0, sdRoundBox(px - fc, vec2(fw * 0.5, uBar.w * 0.5), uBarRadius));
		col.rgb = mix(col.rgb, uBarFill.rgb, cf);
		col.a   = mix(col.a,   uBarFill.a,   cf);
	}

	fragColor = col;
}
