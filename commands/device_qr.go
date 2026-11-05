package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/term"
)

func showDeviceQR(d Dependencies, link string) error {
	if !d.IsTerminal() || d.Getenv("TERM") == "dumb" || d.Getenv("NO_COLOR") != "" || d.ColorEnabled != nil && !d.ColorEnabled() {
		return nil
	}
	encoding := strings.ToLower(strings.Split(d.Getenv("PYTHONIOENCODING"), ":")[0])
	if encoding != "" && encoding != "utf-8" && encoding != "utf8" {
		return nil
	}
	qr, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		return nil //nolint:nilerr // A long link remains usable without a QR code.
	}
	bitmap := qr.Bitmap() // Includes the four-module quiet zone.
	columns, err := strconv.Atoi(d.Getenv("COLUMNS"))
	if err != nil || columns <= 0 {
		columns = 80
		if file, ok := d.Out.(*os.File); ok {
			if width, _, err := term.GetSize(int(file.Fd())); err == nil && width > 0 {
				columns = width
			}
		}
	}
	if len(bitmap) >= columns {
		return nil
	}
	_, err = fmt.Fprint(d.Out, "\nOr scan with your phone:\n"+deviceQRText(bitmap))
	return err
}

// Draw light modules as white blocks on black, independent of terminal theme.
// Each cell carries two vertical modules; match Segno's dark padding below
// the quiet zone when the final row has no second module.
func deviceQRText(bitmap [][]bool) string {
	var out strings.Builder
	for y := 0; y < len(bitmap); y += 2 {
		out.WriteString("\x1b[37m\x1b[40m")
		for x, topDark := range bitmap[y] {
			bottomDark := y+1 == len(bitmap) || bitmap[y+1][x]
			switch {
			case topDark && bottomDark:
				out.WriteByte(' ')
			case topDark:
				out.WriteRune('▄')
			case bottomDark:
				out.WriteRune('▀')
			default:
				out.WriteRune('█')
			}
		}
		out.WriteString("\x1b[0m\n")
	}
	return out.String()
}
