package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mitchellh/go-vnc"
)

func Panic(err error) {
	if err != nil {
		panic(err)
	}
}

func keyDown(c *vnc.ClientConn) {
	Panic(c.KeyEvent(0xfee3, true))
	time.Sleep(200 * time.Millisecond)
	Panic(c.KeyEvent(0xfee3, false))
}

func parseDuration(dur string) time.Duration {
	for suffix, msec := range map[string]time.Duration{
		"ms":   time.Millisecond,
		"msec": time.Millisecond,
		"sec":  time.Second,
		"min":  time.Minute,
		"hour": time.Hour,
	} {
		if !strings.HasSuffix(dur, suffix) {
			continue
		}
		s := dur[:len(dur)-len(suffix)]
		raw, err := strconv.ParseUint(s, 10, 64)
		Panic(err)
		return msec * time.Duration(raw)
	}
	Panic(fmt.Errorf("Cannot parse %s", dur))
	panic("")
}

type vncCon struct {
	conn *vnc.ClientConn
	ch   chan vnc.ServerMessage
}

func newConn(conn string) (*vncCon, error) {
	nc, err := net.Dial("tcp", conn)
	if err != nil {
		return nil, err
	}
	ch := make(chan vnc.ServerMessage)
	cfg := &vnc.ClientConfig{ServerMessageCh: ch}
	c, err := vnc.Client(nc, cfg)
	if err != nil {
		return nil, err
	}
	return &vncCon{c, ch}, nil
}

func getScreenshot(vc *vncCon) *image.RGBA {
	c := vc.conn
	Panic(c.FramebufferUpdateRequest(false, 0, 0, c.FrameBufferWidth, c.FrameBufferHeight))
	r := <-vc.ch
	if r.Type() != 0 {
		Panic(fmt.Errorf("wrong msg"))
	}

	fb := r.(*vnc.FramebufferUpdateMessage)
	if len(fb.Rectangles) != 1 {
		panic("expected to get a single rectangle")
	}
	rect := fb.Rectangles[0]
	raw := rect.Enc.(*vnc.RawEncoding)
	w, h := int(rect.Width), int(rect.Height)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, col := range raw.Colors {
		x := i % w
		y := i / w
		img.Set(x, y, color.RGBA{
			uint8(col.R), uint8(col.G), uint8(col.B), 255})
	}
	return img
}

type imageResult struct {
	img     *image.RGBA
	i       int
	cmd     *exec.Cmd
	vncport int
	err     error
}

func hist(img *image.RGBA) map[color.RGBA]int {
	m := make(map[color.RGBA]int)
	rect := img.Bounds()
	for x := rect.Min.X; x < rect.Max.X; x++ {
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			rgba := img.RGBAAt(x, y)
			if rgba.A == 0 {
				fmt.Printf("%s on %dx%d\n", rgba, x, y)
			}
			//c := uint64(rgba.R) | uint64(rgba.G)<<8 | uint64(rgba.B)<<16
			m[rgba]++
		}
	}
	return m
}

func killPids(r io.Reader, kill9 bool) bool {
	empty := true
	scanner := bufio.NewScanner(r)
	me := os.Getpid()
	for scanner.Scan() {
		empty = false
		pid, err := strconv.ParseUint(scanner.Text(), 10, 64)
		Panic(err)
		if int(pid) == me {
			continue
		}
		p, err := os.FindProcess(int(pid))
		Panic(err)
		p.Kill()
		if kill9 {
			p.Signal(os.Kill)
		}
	}
	return empty
}

const cgroup_dir = "/sys/fs/cgroup/systemd/vnctest"
const cgroup_procs = cgroup_dir + "/cgroup.procs"
const cgroup_tasks = cgroup_dir + "/tasks"

func killCgroup() {
	f, err := os.Open(cgroup_tasks)
	Panic(err)
	killPids(f, false)
	f.Close()
	time.Sleep(time.Second)

	f, err = os.Open(cgroup_tasks)
	Panic(err)
	killPids(f, true)
	f.Close()
}

func main() {
	qemu := flag.String("qemu", "", "QEMU command to run, %p replaced by VNC port %c by counter")
	n := flag.Int("n", 1, "Number of QEMU instances to run")
	baseport := flag.Int("vncbaseport", 77, "VNC port from which to start (5900+vncbaseport)")
	vnchost := flag.String("vnchost", "localhost", "VNC host to connect")
	histogram := flag.String("histogram", "", "print histogram from a specific VNC TCP connection string")
	settle := flag.String("settle", "", "Time duration to wait before checking if QEMU have BSOD")
	nocgroup := flag.Bool("disable-cgroup", false, "should you capture all PIDs in a cgroup, killing previous processes")
	rounds := flag.Int("rounds", 1, "How many times should I run")
	flag.Parse()
	startedAt := time.Now()
	if *histogram != "" {
		c, err := newConn(*histogram)
		Panic(err)
		keyDown(c.conn)
		img := getScreenshot(c)
		h := hist(img)
		fmt.Printf("%d\n", len(h))
		fmt.Printf("%dx%d\n", img.Bounds().Max.X, img.Bounds().Max.Y)
		fmt.Println(h)
		os.Exit(0)
	}
	if *qemu == "" {
		fmt.Fprintln(os.Stderr, "-qemu required")
		flag.Usage()
		os.Exit(1)
	}
	if !*nocgroup {
		if _, err := os.Stat(cgroup_dir); os.IsNotExist(err) {
			Panic(os.Mkdir(cgroup_dir, 0600))
		}

		killCgroup()

		f, err := os.Open(cgroup_tasks)
		Panic(err)
		if !killPids(f, false) {
			fmt.Fprintln(os.Stderr, "Cannot kill previous vnctest processes")
			os.Exit(2)
		}
		f.Close()

		f, err = os.OpenFile(cgroup_procs, os.O_APPEND|os.O_WRONLY, 0600)
		Panic(err)
		f.WriteString(fmt.Sprint(os.Getpid()))
		_, err = f.WriteString(fmt.Sprint(os.Getpid()))
		Panic(err)
	}
	settleDur := parseDuration(*settle)
	result := make(chan *imageResult)
	for {
		for i := 0; i < *n; i++ {
			go func(i int) {
				vncport := fmt.Sprint(*baseport + i)
				vnctcpport := fmt.Sprint(5900 + *baseport + i)
				cmd := strings.Replace(*qemu, "%c", fmt.Sprint(i), -1)
				cmd = strings.Replace(cmd, "%p", vncport, -1)
				cmd = strings.Replace(cmd, "%h", *vnchost, -1)
				p := exec.Command("bash", "-c", cmd)
				Panic(p.Start())
				time.Sleep(settleDur)
				if p.ProcessState != nil && p.ProcessState.Exited() {
					panic("qemu dead")
				}
				c, err := newConn(fmt.Sprintf("%s:%s", *vnchost, vnctcpport))
				keyDown(c.conn)
				if err == nil {
					result <- &imageResult{getScreenshot(c), i, p, *baseport + i, nil}

				} else {
					result <- &imageResult{i: i, err: err}
				}
				p.Wait()
			}(i)
		}
		hists := make([]struct {
			hist, pid, vncport int
			img                string
		}, *n)
		untilNow := time.Now().Sub(startedAt)
		for i := 0; i < *n; i++ {
			r := <-result
			hists[i].vncport = r.vncport
			if r.err != nil {
				fmt.Println("error getting", r.i, r.err)
				continue
			}
			pngFileName := fmt.Sprintf("/tmp/vnctest%03d-pid%d.png", r.i, os.Getpid())
			f, err := os.OpenFile(pngFileName, os.O_WRONLY|os.O_CREATE, 0600)
			hists[i].pid = r.cmd.Process.Pid
			hists[i].hist = len(hist(r.img))
			hists[i].img = pngFileName
			Panic(err)
			png.Encode(f, r.img)
			exec.Command("open", pngFileName).Run()
		}
		for i := 0; i < *n; i++ {
			for hists[i].hist < 30 {
				fmt.Println("Found BSOD in pid", hists[i].pid, "after", untilNow, "screen", hists[i].img, "on VNC port", hists[i].vncport)
				for {
					time.Sleep(120 * time.Minute)
				}
			}
			fmt.Printf("pid %d hist %d ", hists[i].pid, hists[i].hist)
		}
		fmt.Println(untilNow, "passed")
		if *rounds == 0 {
			break
		}
		if *rounds > 0 {
			(*rounds)--
		}
	}
	//nc, err := net.Dial("tcp", "10.0.2.142:5900")
	// 0xff54

}
