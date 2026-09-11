//go:build darwin

package darwin

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/larffxx/singboxui/internal/atomicfile"
	"github.com/larffxx/singboxui/internal/domain/apperr"
)

// Autostart is the macOS login item: a LaunchAgent plist that launchd reads from
// ~/Library/LaunchAgents at login (internal-contracts.md §2).
//
// The entry is written in the same file the Java prototype used and points at the
// native application instead of `java -jar`, so a leftover entry is detected by
// comparing its target with this executable (migration plan §1).
type Autostart struct {
	// executable is the path of the running application, used to tell an entry
	// this application wrote from an entry left behind by the prototype. It is
	// empty when os.Executable failed, in which case any existing entry counts as
	// legacy - reporting it is always safe, ignoring it is not.
	executable string
	// plistPath is ~/Library/LaunchAgents/com.larffxx.singboxui.plist.
	plistPath string
}

// NewAutostart returns the login item of this application.
func NewAutostart() (*Autostart, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, opAutostart, "the home directory cannot be determined", err)
	}
	if home == "" {
		return nil, apperr.New(apperr.CodeInternal, opAutostart, "the home directory cannot be determined")
	}
	self, err := os.Executable()
	if err != nil {
		self = ""
	}
	return &Autostart{
		executable: self,
		plistPath:  filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist"),
	}, nil
}

// Label is the launchd label of the login item.
func (a *Autostart) Label() string { return AgentLabel }

// Path is the plist this login item lives in. The platform port does not expose
// it; the migration and the tests need to know which file is meant.
func (a *Autostart) Path() string { return a.plistPath }

// Supported reports whether the platform can install a login item. A LaunchAgent
// needs a user session and a home directory, both of which are present whenever
// this code runs on macOS.
func (a *Autostart) Supported() bool { return a.plistPath != "" }

// Enabled reports whether the login item is installed.
func (a *Autostart) Enabled() (bool, error) {
	_, err := os.Stat(a.plistPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item cannot be read", err)
	}
	return true, nil
}

// Enable writes the login item so that execPath starts with the next user
// session, replacing whatever entry is there.
//
// No launchctl call is made: launchd loads every plist in ~/Library/LaunchAgents
// at login, RunAtLoad starts the program, and loading the agent now would start a
// second copy of the application in the session the user is already in
// (current-state.md: `launchctl load/unload` is not needed for LaunchAgents).
func (a *Autostart) Enable(execPath string) error {
	if err := validateExecutable(execPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.plistPath), 0o755); err != nil {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the LaunchAgents directory cannot be created", err)
	}
	body, err := renderPlist(execPath)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(a.plistPath, []byte(body), 0o644); err != nil {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item cannot be written", err)
	}
	return nil
}

// Disable removes the login item.
//
// The agent is not booted out of launchd: `launchctl bootout` on this job
// terminates the running application, which is the program the user is looking at
// right now. launchd forgets the job at the next login.
func (a *Autostart) Disable() error {
	if err := os.Remove(a.plistPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return apperr.Wrap(apperr.CodeInternal, opAutostart, "the login item cannot be removed", err)
	}
	return nil
}

// LegacyEntry describes the login item the Java prototype left behind, if one is
// present, and returns "" when the installed entry is this application's own
// (migration plan §1). The description is shown in Settings and can be acted upon
// through RemoveLegacy; nothing here deletes anything by itself.
func (a *Autostart) LegacyEntry() string {
	info, err := os.Stat(a.plistPath)
	if err != nil {
		// No login item at all: nothing was left behind.
		return ""
	}
	if info.IsDir() {
		// A directory where the plist belongs is not a login item anyone can
		// have written, so there is nothing to describe.
		return ""
	}
	target, ok := a.installedTarget()
	if ok && a.executable != "" && sameExecutable(target, a.executable) {
		return ""
	}
	if !ok {
		// The entry exists but does not say which program it starts. Reporting
		// it is the safe side: ignoring it would leave a second login item in
		// place that starts something this application cannot see.
		target = "an unknown program"
	}
	return fmt.Sprintf("the login item %s starts %s", a.plistPath, target)
}

// RemoveLegacy deletes the login item left behind by the prototype. It is the
// explicit user action behind "old Java entry found - replace?" and does nothing
// when there is no legacy entry.
func (a *Autostart) RemoveLegacy() error {
	if a.LegacyEntry() == "" {
		return nil
	}
	return a.Disable()
}

// installedTarget returns the program the installed login item starts.
func (a *Autostart) installedTarget() (string, bool) {
	data, err := os.ReadFile(a.plistPath)
	if err != nil {
		return "", false
	}
	target, ok := programArgument(string(data))
	if !ok || strings.TrimSpace(target) == "" {
		return "", false
	}
	return target, true
}

// programArgument returns the first ProgramArguments program of a plist document,
// decoded back into the path that was written into it.
//
// A plist is XML, but only this one value is ever needed, so the scan is narrow
// on purpose: it looks for the ProgramArguments key and then for the first
// <string> element after it. The value is then run through the XML decoder,
// because a path containing &, < or " is written escaped and must read back as
// the path it is - otherwise the entry this application wrote would not be
// recognised as its own. A document it does not recognise yields no target, which
// makes the entry show up as legacy rather than as this application's.
func programArgument(document string) (string, bool) {
	key := strings.Index(document, "<key>ProgramArguments</key>")
	if key < 0 {
		return "", false
	}
	rest := document[key:]
	open := strings.Index(rest, "<string>")
	if open < 0 {
		return "", false
	}
	rest = rest[open+len("<string>"):]
	end := strings.Index(rest, "</string>")
	if end < 0 {
		return "", false
	}
	return unescapeXML(rest[:end]), true
}

// unescapeXML decodes the character references of a plist string. A value it
// cannot decode is returned as it stands: a hand-written plist may contain a bare
// &, and reporting that text is better than reporting nothing.
func unescapeXML(s string) string {
	var decoded struct {
		Value string `xml:",chardata"`
	}
	if err := xml.Unmarshal([]byte("<value>"+s+"</value>"), &decoded); err != nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(decoded.Value)
}

// renderPlist is the login item document: the application executable, started at
// login, in an Aqua session only, with no keep-alive (the application is a GUI
// program, not a daemon that must be resurrected).
func renderPlist(execPath string) (string, error) {
	var quoted strings.Builder
	if err := xml.EscapeText(&quoted, []byte(execPath)); err != nil {
		return "", apperr.Wrap(apperr.CodeInternal, opAutostart, "the application path cannot be written into the login item", err)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("\t<key>Label</key>\n\t<string>" + AgentLabel + "</string>\n")
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n\t\t<string>" + quoted.String() + "</string>\n\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<false/>\n")
	b.WriteString("\t<key>LimitLoadToSessionType</key>\n\t<string>Aqua</string>\n")
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Interactive</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

// validateExecutable refuses a path that cannot be an application executable, so
// that a malformed request never reaches the plist.
func validateExecutable(path string) error {
	if !filepath.IsAbs(path) {
		return apperr.Newf(apperr.CodeInvalidArgument, opAutostart, "the autostart program must be an absolute path, got %q", path)
	}
	if path != filepath.Clean(path) {
		return apperr.Newf(apperr.CodeInvalidArgument, opAutostart, "the autostart program must be a clean path, got %q", path)
	}
	if hasControl(path) {
		return apperr.New(apperr.CodeInvalidArgument, opAutostart, "the autostart program contains control characters")
	}
	return nil
}

// sameExecutable compares two program paths, following symlinks when they both
// exist: the login item may name the bundle executable while this process was
// started through a symlink to it.
func sameExecutable(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(resolvedA) == filepath.Clean(resolvedB)
}

// hasControl reports whether s contains a control character, which no path of
// this application may contain.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
