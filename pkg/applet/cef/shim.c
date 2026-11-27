//go:build cef

// shim.c implements shim.h on top of CEF's C API.
//
// Reference counting follows the C API rules: a struct CEF passes into
// one of our callbacks is ours to release, a struct a CEF function
// returns is ours to release, and a struct we return to CEF (or pass to
// a CEF function) carries a reference CEF will release.
#include <stdatomic.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>

#include "include/capi/cef_app_capi.h"
#include "include/capi/cef_browser_capi.h"
#include "include/capi/cef_browser_process_handler_capi.h"
#include "include/capi/cef_client_capi.h"
#include "include/capi/cef_life_span_handler_capi.h"
#include "include/capi/cef_render_handler_capi.h"
#include "include/capi/cef_request_capi.h"
#include "include/capi/cef_resource_handler_capi.h"
#include "include/capi/cef_response_capi.h"
#include "include/capi/cef_scheme_capi.h"
#include "include/cef_api_hash.h"

#include "scheme.h"
#include "shim.h"

#include "_cgo_export.h"

#define container_of(ptr, type, member) \
	((type*)((char*)(ptr) - offsetof(type, member)))

static void set_str(cef_string_t* dst, const char* src) {
	if (src == NULL || src[0] == '\0') {
		return;
	}
	cef_string_utf8_to_utf16(src, strlen(src), dst);
}

// Process-wide singletons. CEF may add and release references to them
// freely; they live for the whole process, so counting is a no-op.

static void CEF_CALLBACK static_add_ref(cef_base_ref_counted_t* self) {
	(void)self;
}

static int CEF_CALLBACK static_release(cef_base_ref_counted_t* self) {
	(void)self;
	return 0;
}

static int CEF_CALLBACK static_has_one_ref(cef_base_ref_counted_t* self) {
	(void)self;
	return 1;
}

static void init_static_base(cef_base_ref_counted_t* base, size_t size) {
	base->size = size;
	base->add_ref = static_add_ref;
	base->release = static_release;
	base->has_one_ref = static_has_one_ref;
	base->has_at_least_one_ref = static_has_one_ref;
}

static cef_app_t app;
static cef_browser_process_handler_t process_handler;

static void CEF_CALLBACK on_schedule_message_pump_work(
		cef_browser_process_handler_t* self, int64_t delay_ms) {
	(void)self;
	goSchedulePump(delay_ms);
}

static cef_browser_process_handler_t* CEF_CALLBACK
get_browser_process_handler(cef_app_t* self) {
	(void)self;
	return &process_handler;
}

static void CEF_CALLBACK on_register_custom_schemes(cef_app_t* self,
		cef_scheme_registrar_t* registrar) {
	(void)self;
	tb_register_scheme(registrar);
}

static void init_app(void) {
	memset(&app, 0, sizeof(app));
	init_static_base(&app.base, sizeof(app));
	app.get_browser_process_handler = get_browser_process_handler;
	app.on_register_custom_schemes = on_register_custom_schemes;

	memset(&process_handler, 0, sizeof(process_handler));
	init_static_base(&process_handler.base, sizeof(process_handler));
	process_handler.on_schedule_message_pump_work =
		on_schedule_message_pump_work;
}

// to_utf8 converts a CEF string a getter returned (and frees it) into a
// malloc'd UTF-8 string, never NULL.
static char* to_utf8(cef_string_userfree_t s) {
	cef_string_utf8_t out = {0};
	if (s != NULL) {
		cef_string_utf16_to_utf8(s->str, s->length, &out);
		cef_string_userfree_free(s);
	}
	char* copy = malloc(out.length + 1);
	memcpy(copy, out.str ? out.str : "", out.length);
	copy[out.length] = '\0';
	cef_string_utf8_clear(&out);
	return copy;
}

// read_post_body copies a request's upload bytes into one malloc'd
// buffer. File elements are not supported; pages send bytes.
static uint8_t* read_post_body(cef_request_t* req, size_t* len) {
	*len = 0;
	cef_post_data_t* post = req->get_post_data(req);
	if (post == NULL) {
		return NULL;
	}
	size_t count = post->get_element_count(post);
	cef_post_data_element_t** elems = calloc(count, sizeof(*elems));
	post->get_elements(post, &count, elems);
	size_t total = 0;
	for (size_t i = 0; i < count; i++) {
		total += elems[i]->get_bytes_count(elems[i]);
	}
	uint8_t* body = malloc(total > 0 ? total : 1);
	for (size_t i = 0; i < count; i++) {
		size_t n = elems[i]->get_bytes_count(elems[i]);
		*len += elems[i]->get_bytes(elems[i], n, body + *len);
		elems[i]->base.release(&elems[i]->base);
	}
	free(elems);
	post->base.release(&post->base);
	return body;
}

// tb_resource answers one tubeless:// request with bytes Go produced.
typedef struct tb_resource {
	cef_resource_handler_t handler;
	atomic_int refs;
	int browser_id;
	int status;
	char* mime;
	uint8_t* data;
	size_t len;
	size_t pos;
} tb_resource;

static void CEF_CALLBACK resource_add_ref(cef_base_ref_counted_t* s) {
	atomic_fetch_add(&((tb_resource*)s)->refs, 1);
}

static int CEF_CALLBACK resource_release(cef_base_ref_counted_t* s) {
	tb_resource* r = (tb_resource*)s;
	if (atomic_fetch_sub(&r->refs, 1) != 1) {
		return 0;
	}
	free(r->mime);
	free(r->data);
	free(r);
	return 1;
}

static int CEF_CALLBACK resource_has_one_ref(cef_base_ref_counted_t* s) {
	return atomic_load(&((tb_resource*)s)->refs) == 1;
}

static int CEF_CALLBACK resource_has_refs(cef_base_ref_counted_t* s) {
	return atomic_load(&((tb_resource*)s)->refs) >= 1;
}

static int CEF_CALLBACK resource_open(cef_resource_handler_t* self,
		cef_request_t* req, int* handle_request, cef_callback_t* callback) {
	tb_resource* r = (tb_resource*)self;
	char* url = to_utf8(req->get_url(req));
	char* method = to_utf8(req->get_method(req));
	size_t body_len = 0;
	uint8_t* body = read_post_body(req, &body_len);
	tb_response out = {0};
	goServeScheme(r->browser_id, url, method, body, body_len, &out);
	free(url);
	free(method);
	free(body);
	r->status = out.status;
	r->mime = out.mime;
	r->data = out.data;
	r->len = out.len;
	*handle_request = 1;
	req->base.release(&req->base);
	if (callback != NULL) {
		callback->base.release(&callback->base);
	}
	return 1;
}

static void CEF_CALLBACK resource_headers(cef_resource_handler_t* self,
		cef_response_t* resp, int64_t* length, cef_string_t* redirect) {
	(void)redirect;
	tb_resource* r = (tb_resource*)self;
	resp->set_status(resp, r->status);
	cef_string_t mime = {0};
	set_str(&mime, r->mime);
	resp->set_mime_type(resp, &mime);
	cef_string_clear(&mime);
	cef_string_t key = {0}, value = {0};
	set_str(&key, "Cache-Control");
	set_str(&value, "no-store");
	resp->set_header_by_name(resp, &key, &value, 1);
	cef_string_clear(&key);
	cef_string_clear(&value);
	*length = (int64_t)r->len;
	resp->base.release(&resp->base);
}

static int CEF_CALLBACK resource_read(cef_resource_handler_t* self,
		void* out, int want, int* got, cef_resource_read_callback_t* cb) {
	tb_resource* r = (tb_resource*)self;
	if (cb != NULL) {
		cb->base.release(&cb->base);
	}
	size_t left = r->len - r->pos;
	if (left == 0) {
		*got = 0;
		return 0;
	}
	size_t n = left < (size_t)want ? left : (size_t)want;
	memcpy(out, r->data + r->pos, n);
	r->pos += n;
	*got = (int)n;
	return 1;
}

static void CEF_CALLBACK resource_cancel(cef_resource_handler_t* self) {
	(void)self;
}

static cef_scheme_handler_factory_t scheme_factory;

static cef_resource_handler_t* CEF_CALLBACK scheme_create(
		cef_scheme_handler_factory_t* self, cef_browser_t* browser,
		cef_frame_t* frame, const cef_string_t* scheme, cef_request_t* req) {
	(void)self;
	(void)scheme;
	tb_resource* r = calloc(1, sizeof(tb_resource));
	atomic_init(&r->refs, 1);
	r->browser_id = browser != NULL ? browser->get_identifier(browser) : -1;
	r->handler.base.size = sizeof(cef_resource_handler_t);
	r->handler.base.add_ref = resource_add_ref;
	r->handler.base.release = resource_release;
	r->handler.base.has_one_ref = resource_has_one_ref;
	r->handler.base.has_at_least_one_ref = resource_has_refs;
	r->handler.open = resource_open;
	r->handler.get_response_headers = resource_headers;
	r->handler.read = resource_read;
	r->handler.cancel = resource_cancel;
	if (browser != NULL) {
		browser->base.release(&browser->base);
	}
	if (frame != NULL) {
		frame->base.release(&frame->base);
	}
	if (req != NULL) {
		req->base.release(&req->base);
	}
	return &r->handler;
}

static void register_scheme_factory(void) {
	memset(&scheme_factory, 0, sizeof(scheme_factory));
	init_static_base(&scheme_factory.base, sizeof(scheme_factory));
	scheme_factory.create = scheme_create;
	cef_string_t name = {0};
	set_str(&name, TB_SCHEME);
	cef_register_scheme_handler_factory(&name, NULL, &scheme_factory);
	cef_string_clear(&name);
}

int tb_cef_init(const tb_options* opts) {
	cef_api_hash(CEF_API_VERSION, 0);
	init_app();

	cef_settings_t s;
	memset(&s, 0, sizeof(s));
	s.size = sizeof(s);
	s.no_sandbox = 1;
	s.windowless_rendering_enabled = 1;
	s.external_message_pump = 1;
	s.command_line_args_disabled = 1;
	s.disable_signal_handlers = 1;
	s.log_severity = LOGSEVERITY_WARNING;
	s.background_color = opts->background;
	set_str(&s.browser_subprocess_path, opts->subprocess_path);
	set_str(&s.resources_dir_path, opts->resources_dir);
	set_str(&s.locales_dir_path, opts->locales_dir);
	set_str(&s.root_cache_path, opts->cache_dir);
	set_str(&s.cache_path, opts->cache_dir);
	set_str(&s.log_file, opts->log_file);

	static char* argv[] = {"tubeless", NULL};
	cef_main_args_t args = {1, argv};
	int ok = cef_initialize(&args, &s, &app, NULL);
	if (ok) {
		register_scheme_factory();
	}

	cef_string_clear(&s.browser_subprocess_path);
	cef_string_clear(&s.resources_dir_path);
	cef_string_clear(&s.locales_dir_path);
	cef_string_clear(&s.root_cache_path);
	cef_string_clear(&s.cache_path);
	cef_string_clear(&s.log_file);
	return ok;
}

void tb_cef_do_work(void) {
	cef_do_message_loop_work();
}

void tb_cef_shutdown(void) {
	cef_shutdown();
}

// tb_browser is one offscreen browser. Its three handlers share one
// reference count: the struct is freed when CEF and Go have both let
// go of every one of them.
struct tb_browser {
	cef_client_t client;
	cef_render_handler_t render;
	cef_life_span_handler_t life;
	atomic_int refs;
	uintptr_t handle;
	int width;
	int height;
	float scale;
	cef_browser_t* browser;
};

static void browser_add_ref(tb_browser* b) {
	atomic_fetch_add(&b->refs, 1);
}

static int browser_release(tb_browser* b) {
	if (atomic_fetch_sub(&b->refs, 1) != 1) {
		return 0;
	}
	free(b);
	return 1;
}

static int browser_has_one_ref(tb_browser* b) {
	return atomic_load(&b->refs) == 1;
}

static int browser_has_refs(tb_browser* b) {
	return atomic_load(&b->refs) >= 1;
}

#define TB_REFCOUNT(name, field, type)                                       \
	static void CEF_CALLBACK name##_add_ref(cef_base_ref_counted_t* s) {   \
		browser_add_ref(container_of((type*)s, tb_browser, field));        \
	}                                                                      \
	static int CEF_CALLBACK name##_release(cef_base_ref_counted_t* s) {    \
		return browser_release(container_of((type*)s, tb_browser, field)); \
	}                                                                      \
	static int CEF_CALLBACK name##_has_one_ref(cef_base_ref_counted_t* s) { \
		return browser_has_one_ref(                                        \
			container_of((type*)s, tb_browser, field));                    \
	}                                                                      \
	static int CEF_CALLBACK name##_has_refs(cef_base_ref_counted_t* s) {   \
		return browser_has_refs(container_of((type*)s, tb_browser, field)); \
	}                                                                      \
	static void name##_init_base(cef_base_ref_counted_t* base) {           \
		base->size = sizeof(type);                                         \
		base->add_ref = name##_add_ref;                                    \
		base->release = name##_release;                                    \
		base->has_one_ref = name##_has_one_ref;                            \
		base->has_at_least_one_ref = name##_has_refs;                      \
	}

TB_REFCOUNT(client, client, cef_client_t)
TB_REFCOUNT(render, render, cef_render_handler_t)
TB_REFCOUNT(life, life, cef_life_span_handler_t)

static void release_browser_arg(cef_browser_t* browser) {
	if (browser != NULL) {
		browser->base.release(&browser->base);
	}
}

static cef_render_handler_t* CEF_CALLBACK get_render_handler(
		cef_client_t* self) {
	tb_browser* b = container_of(self, tb_browser, client);
	browser_add_ref(b);
	return &b->render;
}

static cef_life_span_handler_t* CEF_CALLBACK get_life_span_handler(
		cef_client_t* self) {
	tb_browser* b = container_of(self, tb_browser, client);
	browser_add_ref(b);
	return &b->life;
}

static int dip(int px, float scale) {
	int v = (int)((float)px / scale + 0.5f);
	return v < 1 ? 1 : v;
}

static void CEF_CALLBACK get_view_rect(cef_render_handler_t* self,
		cef_browser_t* browser, cef_rect_t* rect) {
	tb_browser* b = container_of(self, tb_browser, render);
	rect->x = 0;
	rect->y = 0;
	rect->width = dip(b->width, b->scale);
	rect->height = dip(b->height, b->scale);
	release_browser_arg(browser);
}

static int CEF_CALLBACK get_screen_info(cef_render_handler_t* self,
		cef_browser_t* browser, cef_screen_info_t* info) {
	tb_browser* b = container_of(self, tb_browser, render);
	info->device_scale_factor = b->scale;
	info->rect.x = 0;
	info->rect.y = 0;
	info->rect.width = dip(b->width, b->scale);
	info->rect.height = dip(b->height, b->scale);
	info->available_rect = info->rect;
	release_browser_arg(browser);
	return 1;
}

static void CEF_CALLBACK on_paint(cef_render_handler_t* self,
		cef_browser_t* browser, cef_paint_element_type_t type,
		size_t rect_count, cef_rect_t const* rects, const void* buffer,
		int width, int height) {
	tb_browser* b = container_of(self, tb_browser, render);
	if (type == PET_VIEW) {
		goOnPaint(b->handle, (int*)rects, (int)rect_count, (void*)buffer,
			width, height);
	}
	release_browser_arg(browser);
}

static void CEF_CALLBACK on_after_created(cef_life_span_handler_t* self,
		cef_browser_t* browser) {
	tb_browser* b = container_of(self, tb_browser, life);
	// Keep the reference CEF handed us: it is how every later call
	// reaches the browser host. on_before_close drops it.
	b->browser = browser;
	goOnCreated(b->handle, browser->get_identifier(browser));
}

static void CEF_CALLBACK on_before_close(cef_life_span_handler_t* self,
		cef_browser_t* browser) {
	tb_browser* b = container_of(self, tb_browser, life);
	cef_browser_t* kept = b->browser;
	b->browser = NULL;
	release_browser_arg(kept);
	release_browser_arg(browser);
	goOnClosed(b->handle);
}

tb_browser* tb_browser_create(uintptr_t handle, const char* url,
		int width, int height, float scale, uint32_t background, int fps) {
	tb_browser* b = calloc(1, sizeof(tb_browser));
	if (b == NULL) {
		return NULL;
	}
	atomic_init(&b->refs, 1);
	b->handle = handle;
	b->width = width;
	b->height = height;
	b->scale = scale;

	client_init_base(&b->client.base);
	b->client.get_render_handler = get_render_handler;
	b->client.get_life_span_handler = get_life_span_handler;

	render_init_base(&b->render.base);
	b->render.get_view_rect = get_view_rect;
	b->render.get_screen_info = get_screen_info;
	b->render.on_paint = on_paint;

	life_init_base(&b->life.base);
	b->life.on_after_created = on_after_created;
	b->life.on_before_close = on_before_close;

	cef_window_info_t wi;
	memset(&wi, 0, sizeof(wi));
	wi.size = sizeof(wi);
	wi.windowless_rendering_enabled = 1;

	cef_browser_settings_t bs;
	memset(&bs, 0, sizeof(bs));
	bs.size = sizeof(bs);
	bs.windowless_frame_rate = fps;
	bs.background_color = background;

	cef_string_t u;
	memset(&u, 0, sizeof(u));
	set_str(&u, url);

	// The client reference passed in is CEF's to release.
	browser_add_ref(b);
	int ok = cef_browser_host_create_browser(&wi, &b->client, &u, &bs,
		NULL, NULL);
	cef_string_clear(&u);
	if (!ok) {
		browser_release(b);
		return NULL;
	}
	return b;
}

// get_host returns a new reference to the browser's host, or NULL until
// CEF has created the browser (or after it closed).
static cef_browser_host_t* get_host(tb_browser* b) {
	if (b->browser == NULL) {
		return NULL;
	}
	return b->browser->get_host(b->browser);
}

static void release_host(cef_browser_host_t* host) {
	host->base.release(&host->base);
}

void tb_browser_resize(tb_browser* b, int width, int height, float scale) {
	int rescaled = scale != b->scale;
	b->width = width;
	b->height = height;
	b->scale = scale;
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	if (rescaled) {
		host->notify_screen_info_changed(host);
	}
	host->was_resized(host);
	release_host(host);
}

void tb_browser_close(tb_browser* b) {
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	host->close_browser(host, 1);
	release_host(host);
}

void tb_browser_free(tb_browser* b) {
	browser_release(b);
}

void tb_browser_focus(tb_browser* b, int focused) {
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	host->set_focus(host, focused);
	release_host(host);
}

void tb_browser_mouse_move(tb_browser* b, int x, int y, uint32_t mods,
		int leave) {
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	cef_mouse_event_t ev = {x, y, mods};
	host->send_mouse_move_event(host, &ev, leave);
	release_host(host);
}

void tb_browser_mouse_click(tb_browser* b, int x, int y, uint32_t mods,
		int button, int up, int count) {
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	cef_mouse_event_t ev = {x, y, mods};
	host->send_mouse_click_event(host, &ev,
		(cef_mouse_button_type_t)button, up, count);
	release_host(host);
}

void tb_browser_mouse_wheel(tb_browser* b, int x, int y, uint32_t mods,
		int dx, int dy) {
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	cef_mouse_event_t ev = {x, y, mods};
	host->send_mouse_wheel_event(host, &ev, dx, dy);
	release_host(host);
}

void tb_browser_key(tb_browser* b, const tb_key* key) {
	cef_browser_host_t* host = get_host(b);
	if (host == NULL) {
		return;
	}
	cef_key_event_t ev;
	memset(&ev, 0, sizeof(ev));
	ev.size = sizeof(ev);
	ev.type = (cef_key_event_type_t)key->type;
	ev.modifiers = key->modifiers;
	ev.windows_key_code = key->windows_key_code;
	ev.native_key_code = key->native_key_code;
	ev.character = key->character;
	ev.unmodified_character = key->unmodified_character;
	host->send_key_event(host, &ev);
	release_host(host);
}

void tb_browser_exec_js(tb_browser* b, const char* code) {
	if (b->browser == NULL) {
		return;
	}
	cef_frame_t* frame = b->browser->get_main_frame(b->browser);
	if (frame == NULL) {
		return;
	}
	cef_string_t js = {0}, url = {0};
	set_str(&js, code);
	set_str(&url, "tubeless://bridge");
	frame->execute_java_script(frame, &js, &url, 0);
	cef_string_clear(&js);
	cef_string_clear(&url);
	frame->base.release(&frame->base);
}
