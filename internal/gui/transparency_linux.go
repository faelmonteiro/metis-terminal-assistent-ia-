//go:build linux

package gui

/*
#cgo pkg-config: gtk+-3.0
#cgo LDFLAGS: -ldl
#include <gtk/gtk.h>
#include <dlfcn.h>

typedef struct {
    double red;
    double green;
    double blue;
    double alpha;
} NativeRGBA;

typedef void (*set_bg_color_fn)(void *view, NativeRGBA *rgba);

static void apply_gtk_window_transparency(void *win_ptr, double opacity) {
    if (!win_ptr) return;
    GtkWidget *win = GTK_WIDGET(win_ptr);
    if (!GTK_IS_WIDGET(win)) return;

    // 1. Aplica suporte RGBA ao visual do GtkWindow para permitir transparência real
    GdkScreen *screen = gtk_widget_get_screen(win);
    if (screen) {
        GdkVisual *visual = gdk_screen_get_rgba_visual(screen);
        if (visual) {
            gtk_widget_set_visual(win, visual);
        }
    }
    gtk_widget_set_app_paintable(win, TRUE);

    // 2. Define a opacidade global da janela (equivalente ao setWindowOpacity do Qt no Metis)
    if (opacity >= 0.20 && opacity <= 1.0) {
        gtk_widget_set_opacity(win, opacity);
    }

    // 3. Define o background do WebKitWebView como transparente para que o canal alpha do CSS e do GTK apareçam
    if (GTK_IS_BIN(win)) {
        GtkWidget *child = gtk_bin_get_child(GTK_BIN(win));
        if (child) {
            set_bg_color_fn set_bg = (set_bg_color_fn)dlsym(RTLD_DEFAULT, "webkit_web_view_set_background_color");
            if (set_bg) {
                NativeRGBA transparent = {0.0, 0.0, 0.0, 0.0};
                set_bg(child, &transparent);
            }
        }
    }
}

static void hide_gtk_window(void *win_ptr) {
    if (!win_ptr) return;
    GtkWidget *win = GTK_WIDGET(win_ptr);
    if (GTK_IS_WIDGET(win)) {
        gtk_widget_hide(win);
    }
}

static void show_gtk_window(void *win_ptr) {
    if (!win_ptr) return;
    GtkWidget *win = GTK_WIDGET(win_ptr);
    if (GTK_IS_WIDGET(win)) {
        gtk_widget_show_all(win);
    }
}
*/
import "C"
import "unsafe"

func applyWindowTransparency(win unsafe.Pointer, opacityPercent int) {
	if win == nil {
		return
	}
	if opacityPercent <= 0 || opacityPercent > 100 {
		opacityPercent = 85
	}
	alpha := float64(opacityPercent) / 100.0
	if alpha < 0.30 {
		alpha = 0.30
	} else if alpha > 1.0 {
		alpha = 1.0
	}
	C.apply_gtk_window_transparency(win, C.double(alpha))
}

func hideWindow(win unsafe.Pointer) {
	if win == nil {
		return
	}
	C.hide_gtk_window(win)
}

func showWindow(win unsafe.Pointer) {
	if win == nil {
		return
	}
	C.show_gtk_window(win)
}
