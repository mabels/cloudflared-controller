package utils

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

type Klog2zeroLog struct {
	// zlog     *zerolog.Logger
	piR *io.PipeReader
	piW *io.PipeWriter
}

type klogLine struct {
	Level    string
	Nr       int
	Time     time.Time
	Pid      string
	Filename string
	Line     int
	Msg      string
}

func ConnectKlog2ZeroLog(zlog *zerolog.Logger) Klog2zeroLog {
	pr, pw := io.Pipe()
	ret := Klog2zeroLog{
		piR: pr,
		piW: pw,
	}
	go func() {
		lineScanner := bufio.NewScanner(pr)
		lineScanner.Split(bufio.ScanLines)
		klogParser := regexp.MustCompile(`^(.)(\d+)\s+(\d+):(\d+):(\d+).(\d+)\s+(\d+)\s+([^:]+):(\d+)]\s+(.*)$`)
		atoi := func(str string) int {
			i, _ := strconv.Atoi(str)
			return i
		}
		for lineScanner.Scan() {
			// I0515 12:32:13.280257       1 leaderelection.go:245] attempting to acquire leader lease default/cloudflared-controller...
			line := lineScanner.Text()
			parsed := klogParser.FindStringSubmatch(line)
			now := time.Now()
			if len(parsed) > 0 {
				kll := klogLine{
					Level: parsed[1],
					Nr:    atoi(parsed[2]),
					Time: time.Date(now.Year(), now.Month(), now.Day(),
						atoi(parsed[3]), atoi(parsed[4]), atoi(parsed[5]),
						atoi(parsed[6])*int(time.Microsecond), now.Location()),
					Pid:      parsed[7],
					Filename: parsed[8],
					Line:     atoi(parsed[9]),
					Msg:      parsed[10],
				}
				var mye *zerolog.Event
				switch kll.Level {
				case "I":
					mye = zlog.Info()
				case "D":
					mye = zlog.Debug()
				case "W":
					mye = zlog.Warn()
				case "E":
					mye = zlog.Error()
				default:
					mye = zlog.Error()
				}
				mye.Any("klog", kll).Msg("")
			} else {
				zlog.Warn().Str("klog", line).Msg("klog could not parsed")
			}
		}
	}()
	return ret
}

func (kz Klog2zeroLog) Write(in []byte) (int, error) {
	kz.piW.Write(in)
	return len(in), nil
}
