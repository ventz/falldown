// Command falldown plays Fall Down, the TI-83 Plus game, on the desktop.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/ventz/falldown/internal/calc"
	"github.com/ventz/falldown/internal/game"
	"github.com/ventz/falldown/internal/link"
	"github.com/ventz/falldown/internal/ui"
)

const keysHelp = `Keys (calculator -> keyboard):
  Left / Right   arrow keys
  2nd            Space, Enter, Z
  ALPHA          A, Tab        (title: toggle speed)
  Right          right arrow   (title, v1.65: toggle 1/2 players)
  MODE           M             (1-player game: turn the calculator off / pause)
  ON             O             (wake up after MODE)
  CLEAR          Esc, Backspace, Q
  G toggles LCD ghosting, F11 or Alt+Enter toggles full screen,
  H hides or shows the key strip.`

func main() {
	version := flag.String("version", "1.65", "game release to play: 1.5 (classic) or 1.65 (adds 2-player link)")
	model := flag.String("model", "83p", "calculator timing: 83p (TI-83 Plus, 120 Hz) or 84p (TI-83 Plus SE / TI-84 Plus, 110 Hz)")
	scale := flag.Int("scale", 7, "initial window size in screen pixels per LCD pixel")
	ghost := flag.Duration("ghost", 12*time.Millisecond, "LCD ghosting time constant; 0 for instant pixels")
	host := flag.String("host", "", "2-player: listen for the other player on this address (default :8383)")
	join := flag.String("join", "", "2-player: connect to a host at this address")
	fullscreen := flag.Bool("fullscreen", false, "start in full screen")
	shot := flag.String("shot", "", "render the title, a game frame and the score screen as PNGs into this directory, then exit")
	seed := flag.Uint64("seed", 0, "random seed for the brick layout (0 = random)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: falldown [flags]\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n%s\n", keysHelp)
	}
	flag.Parse()

	v, ok := game.VariantByVersion(*version)
	if !ok {
		log.Fatalf("unknown -version %q (want 1.5 or 1.65)", *version)
	}
	var m calc.Model
	switch *model {
	case "83p", "83+", "ti83p":
		m = calc.TI83Plus
	case "84p", "84+", "ti84p", "83pse":
		m = calc.TI84Plus
	default:
		log.Fatalf("unknown -model %q (want 83p or 84p)", *model)
	}
	if *scale < 1 {
		log.Fatalf("-scale must be 1 or more, got %d", *scale)
	}
	if !v.Multiplayer && (*host != "" || *join != "") {
		log.Fatalf("-host and -join need -version 1.65; release %s is single-player", v.Version)
	}

	if *shot != "" {
		if err := screenshots(*shot, v, m, *seed); err != nil {
			log.Fatal(err)
		}
		return
	}

	var cable *link.Cable
	switch {
	case *host != "" && *join != "":
		log.Fatal("use -host or -join, not both")
	case *join != "":
		cable = link.NewGuest(*join)
	case *host != "":
		cable = link.NewHost(*host)
	case v.Multiplayer && runtime.GOOS != "js": // a browser can't open a TCP port
		cable = link.NewHost(":" + link.DefaultPort)
	}
	if cable != nil {
		defer cable.Close()
	}

	fmt.Println(keysHelp)
	if cable != nil {
		fmt.Printf("\n2-player link: %s. Choose Players: 2 on the title screen.\n", cable.Describe())
	}

	machine := calc.NewMachine(m, true, *seed)
	var gameCable game.Cable
	if cable != nil {
		gameCable = cable
	}
	g := game.New(machine, v, gameCable, game.DefaultStore())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		g.Run()
	}()

	ebiten.SetWindowTitle("Fall Down")
	// The extra height is for the key strip above the LCD.
	ebiten.SetWindowSize(calc.Width**scale*10/9+16, calc.Height**scale*10/9+16+24)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(ui.TPS)
	ebiten.SetFullscreen(*fullscreen)
	err := ebiten.RunGame(ui.New(machine, finished, *ghost))
	machine.Quit()
	<-finished
	if err != nil {
		log.Fatal(err)
	}
}
