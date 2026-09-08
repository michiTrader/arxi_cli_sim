package term

import (
	"os"
	"strings"
)

// IsTermux reports whether the terminal drawing this session is Termux's own view on
// Android. It is one question with one caller, and the answer changes exactly one
// decision: whether the program claims the mouse.
//
// Everywhere else the mouse is left to the terminal, because a pointer that drags is how a
// reader selects text and the wheel has keyboard spellings that cost nothing to learn. On a
// phone neither half of that holds. A finger has no drag to protect — Termux selects with a
// long press and the handles it draws, and it asks the program's permission for neither — and
// a swipe with the mouse released is not a wheel that does nothing, it is the input's history
// being walked: Termux scrolls the alternate buffer by synthesizing the arrow keys, byte for
// byte the ones a reader would have pressed, so nothing downstream can tell a swipe from a
// press of up. DECSET 1007 is the switch that stops exactly this on a desktop, and Termux
// never consults it. Claiming tracking is the only fix there is, and it is free.
//
// TERMUX_VERSION is the app's own signal, exported by the process that draws the view. PREFIX
// under com.termux is the bootstrap's path, which nothing else has a reason to name, and it is
// the fallback for a launcher or a version that does not export the first.
//
// An ssh session is not this terminal, in either direction, and the guard is for the direction
// that would otherwise be wrong: Termux's sshd inherits TERMUX_VERSION from the session it was
// started in and passes it to every login, so a laptop connected to a phone would have
// drag-to-select taken off a mouse that has one. The other direction — ssh out of Termux into
// a machine that runs this — is a miss rather than a mistake, and so is a proot distro with an
// environment of its own. -mouse answers both, which is what the flag has always been for.
func IsTermux() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	return strings.Contains(os.Getenv("PREFIX"), "com.termux")
}
