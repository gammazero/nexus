package main

import (
	"fmt"
	"log"

	"github.com/rthornton128/goncurses"
)

const (
	maxNameLen     = 32
	maxMessageLen  = 256
	messageWinSize = 3
	prompt         = "> "
)

type chatWin struct {
	msgs             chan message
	msgRows, msgCols int
	username         string
	line             string
}

func newChatWin(username string, msgs chan message) *chatWin {
	return &chatWin{username: username, msgs: msgs}
}

func (cw chatWin) moveLeft(y, x int) (int, int) {
	if x <= len(prompt) && y <= cw.msgCols-messageWinSize {
		return y, x
	}
	if x == 0 {
		x = cw.msgRows
		y--
	} else {
		x--
	}
	return y, x
}

func (cw chatWin) moveRight(y, x int) (int, int) {
	if x == len(prompt) && y == messageWinSize {
		return y, x
	}
	if x == cw.msgRows {
		x = 0
		y++
	} else {
		x++
	}
	return y, x
}

func (cw *chatWin) handleInput(win *goncurses.Window) (line string) {
	k := win.GetChar()
	if k == 0 {
		return ""
	}

	// some systems send a DEL ASCII character instead of KEY_BACKSPACE
	// account for both just in case
	if k == 127 {
		k = goncurses.KEY_BACKSPACE
	}

	switch k {
	case goncurses.KEY_TAB: // tab
	// case goncurses.KEY_RETURN: // enter key vs. KEY_ENTER
	case goncurses.KEY_DOWN: // down arrow key
	case goncurses.KEY_UP: // up arrow key
	case goncurses.KEY_LEFT: // left arrow key
	case goncurses.KEY_RIGHT: // right arrow key
	case goncurses.KEY_HOME: // home key
	case goncurses.KEY_BACKSPACE: // backpace
		if len(cw.line) > 0 {
			cw.line = cw.line[:len(cw.line)-1]
		}
		y, x := win.CursorYX()
		// TODO: handle this more elegantly (e.g. tell ncurses not to print backspaces)
		// we have to do this three times because ncurses inserts two characters for backspace
		// this is likely wrong in some cases
		for i := 0; i < 3; i++ {
			y, x = cw.moveLeft(y, x)
			win.MoveDelChar(y, x)
		}
		win.Refresh()
	case goncurses.KEY_F1: // F1 key
	case goncurses.KEY_F2: // F2 key
	case goncurses.KEY_F3: // F3 key
	case goncurses.KEY_F4: // F4 key
	case goncurses.KEY_F5: // F5 key
	case goncurses.KEY_F6: // F6 key
	case goncurses.KEY_F7: // F7 key
	case goncurses.KEY_F8: // F8 key
	case goncurses.KEY_F9: // F9 key
	case goncurses.KEY_F10: // F10 key
	case goncurses.KEY_F11: // F11 key
	case goncurses.KEY_F12: // F12 key
	case goncurses.KEY_DL: // delete-line key
	case goncurses.KEY_IL: // insert-line key
	case goncurses.KEY_DC: // delete-character key
	case goncurses.KEY_IC: // insert-character key
	case goncurses.KEY_EIC: // sent by rmir or smir in insert mode
	case goncurses.KEY_CLEAR: // clear-screen or erase key3
	case goncurses.KEY_EOS: // clear-to-end-of-screen key
	case goncurses.KEY_EOL: // clear-to-end-of-line key
	case goncurses.KEY_SF: // scroll-forward key
	case goncurses.KEY_SR: // scroll-backward key
	case goncurses.KEY_PAGEDOWN: // page-down key (next-page)
	case goncurses.KEY_PAGEUP: // page-up key (prev-page)
	case goncurses.KEY_STAB: // set-tab key
	case goncurses.KEY_CTAB: // clear-tab key
	case goncurses.KEY_CATAB: // clear-all-tabs key
	case goncurses.KEY_RETURN: // enter key vs. KEY_ENTER
		fallthrough
	case goncurses.KEY_ENTER: // enter/send key
		line = cw.line
		cw.line = ""
		win.Erase()
		win.Print(prompt)
	case goncurses.KEY_PRINT: // print key
	case goncurses.KEY_LL: // lower-left key (home down)
	case goncurses.KEY_A1: // upper left of keypad
	case goncurses.KEY_A3: // upper right of keypad
	case goncurses.KEY_B2: // center of keypad
	case goncurses.KEY_C1: // lower left of keypad
	case goncurses.KEY_C3: // lower right of keypad
	case goncurses.KEY_BTAB: // back-tab key
	case goncurses.KEY_BEG: // begin key
	case goncurses.KEY_CANCEL: // cancel key
	case goncurses.KEY_CLOSE: // close key
	case goncurses.KEY_COMMAND: // command key
	case goncurses.KEY_COPY: // copy key
	case goncurses.KEY_CREATE: // create key
	case goncurses.KEY_END: // end key
	case goncurses.KEY_EXIT: // exit key
	case goncurses.KEY_FIND: // find key
	case goncurses.KEY_HELP: // help key
	case goncurses.KEY_MARK: // mark key
	case goncurses.KEY_MESSAGE: // message key
	case goncurses.KEY_MOVE: // move key
	case goncurses.KEY_NEXT: // next key
	case goncurses.KEY_OPEN: // open key
	case goncurses.KEY_OPTIONS: // options key
	case goncurses.KEY_PREVIOUS: // previous key
	case goncurses.KEY_REDO: // redo key
	case goncurses.KEY_REFERENCE: // reference key
	case goncurses.KEY_REFRESH: // refresh key
	case goncurses.KEY_REPLACE: // replace key
	case goncurses.KEY_RESTART: // restart key
	case goncurses.KEY_RESUME: // resume key
	case goncurses.KEY_SAVE: // save key
	case goncurses.KEY_SBEG: // shifted begin key
	case goncurses.KEY_SCANCEL: // shifted cancel key
	case goncurses.KEY_SCOMMAND: // shifted command key
	case goncurses.KEY_SCOPY: // shifted copy key
	case goncurses.KEY_SCREATE: // shifted create key
	case goncurses.KEY_SDC: // shifted delete-character key
	case goncurses.KEY_SDL: // shifted delete-line key
	case goncurses.KEY_SELECT: // select key
	case goncurses.KEY_SEND: // shifted end key
	case goncurses.KEY_SEOL: // shifted clear-to-end-of-line key
	case goncurses.KEY_SEXIT: // shifted exit key
	case goncurses.KEY_SFIND: // shifted find key
	case goncurses.KEY_SHELP: // shifted help key
	case goncurses.KEY_SHOME: // shifted home key
	case goncurses.KEY_SIC: // shifted insert-character key
	case goncurses.KEY_SLEFT: // shifted left-arrow key
	case goncurses.KEY_SMESSAGE: // shifted message key
	case goncurses.KEY_SMOVE: // shifted move key
	case goncurses.KEY_SNEXT: // shifted next key
	case goncurses.KEY_SOPTIONS: // shifted options key
	case goncurses.KEY_SPREVIOUS: // shifted previous key
	case goncurses.KEY_SPRINT: // shifted print key
	case goncurses.KEY_SREDO: // shifted redo key
	case goncurses.KEY_SREPLACE: // shifted replace key
	case goncurses.KEY_SRIGHT: // shifted right-arrow key
	case goncurses.KEY_SRSUME: // shifted resume key
	case goncurses.KEY_SSAVE: // shifted save key
	case goncurses.KEY_SSUSPEND: // shifted suspend key
	case goncurses.KEY_SUNDO: // shifted undo key
	case goncurses.KEY_SUSPEND: // suspend key
	case goncurses.KEY_UNDO: // undo key
	case goncurses.KEY_MOUSE: // any mouse event
	case goncurses.KEY_RESIZE: // Terminal resize event
	//case goncurses.KEY_EVENT: // We were interrupted by an event
	case goncurses.KEY_MAX:
	default:
		cw.line += goncurses.KeyString(k)
	}
	return
}

func (cw *chatWin) dialog(messages chan message) {
	stdscr, err := goncurses.Init()
	if err != nil {
		log.Fatal("init:", err)
	}
	defer goncurses.End()

	goncurses.Raw(false)
	stdscr.ScrollOk(true)

	rows, cols := stdscr.MaxYX()
	chats, err := goncurses.NewWindow(rows-messageWinSize, cols, 0, 0)
	if err != nil {
		log.Fatal("Error setting up chat window")
	}

	chats.ScrollOk(true)
	chats.Box(goncurses.ACS_VLINE, goncurses.ACS_HLINE)
	chats.Refresh()

	messageWin, err := goncurses.NewWindow(messageWinSize, cols, rows-messageWinSize, 0)
	if err != nil {
		log.Fatal("Error setting up chat window")
	}
	cw.msgRows, cw.msgCols = messageWin.MaxYX()
	// allow special characters to come through (e.g. arrow keys)
	messageWin.Keypad(true)
	// sets terminal to non-blocking
	// this also seems to print characters as they're typed, which causes some weird workarounds
	goncurses.HalfDelay(1)

	messageWin.Print(prompt)
	for {
		select {
		case msg := <-messages:
			y, _ := chats.CursorYX()
			chats.MovePrint(y+1, 1, fmt.Sprint(msg.From, ": ", msg.Message))
			chats.Refresh()
			messageWin.Refresh()
		default:
			line := cw.handleInput(messageWin)
			if line != "" {
				log.Println("Entered message:", line)
				msg := message{From: cw.username, Message: line}
				y, _ := chats.CursorYX()
				chats.MovePrint(y+1, 1, fmt.Sprint(msg.From, ": ", msg.Message))
				chats.Refresh()
				cw.msgs <- msg
			}
		}
	}
}
