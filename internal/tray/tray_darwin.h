#ifndef SINGBOXUI_TRAY_DARWIN_H
#define SINGBOXUI_TRAY_DARWIN_H

// sbuTrayShow creates the status item if the application does not have one yet
// and installs the model. The model is a JSON object:
//
//	{"icon":"idle","tooltip":"…",
//	 "items":[{"id":"show-window","title":"…","enabled":true},
//	          {"separator":true}]}
//
// It returns immediately: Cocoa work happens on the main thread.
void sbuTrayShow(const char *modelJSON);

// sbuTrayHide removes the status item.
void sbuTrayHide(void);

// sbuTraySelect is implemented in Go. It receives the identifier of the menu
// item the user chose, and is called on the main thread.
extern void sbuTraySelect(char *identifier);

// sbuTrayReady is implemented in Go. It is called once, on the main thread, when
// the status item is in the menu bar.
extern void sbuTrayReady(void);

// sbuTrayProblem is implemented in Go. It reports, on the main thread, that the
// model was refused or that the icon could not be drawn.
extern void sbuTrayProblem(char *reason);

#endif /* SINGBOXUI_TRAY_DARWIN_H */
