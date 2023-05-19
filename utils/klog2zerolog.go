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
	// Msg      string
}

// 2023-05-19T12:06:44Z ERR update check failed
var reSimpleZerologParser = regexp.MustCompile(`^([^\s]+)\s+(\S+)\s+(.*)$`)

func TransfromSimpleZeroLogLine(line string, zlog *zerolog.Logger) {
	parsed := reSimpleZerologParser.FindStringSubmatch(line)
	if len(parsed) > 0 {
		var mye *zerolog.Event
		switch parsed[2] {
		case "INF":
			mye = zlog.Info()
		case "DEB":
			mye = zlog.Debug()
		case "WAR":
			mye = zlog.Warn()
		case "ERR":
			mye = zlog.Error()
		default:
			mye = zlog.Error()
		}
		// now, err := time.Parse(time.RFC3339, parsed[1])
		// if err != nil {
		// now = time.Now()
		// }
		mye.Msg(parsed[3])
	} else {
		zlog.Warn().Str("line", line).Msg("line could not parsed")
	}
}

// I0515 12:32:13.280257       1 leaderelection.go:245] attempting to acquire leader lease default/cloudflared-controller...
var reKlogParser = regexp.MustCompile(`^(.)(\d+)\s+(\d+):(\d+):(\d+).(\d+)\s+(\d+)\s+([^:]+):(\d+)]\s+(.*)$`)

func atoi(str string) int {
	i, _ := strconv.Atoi(str)
	return i
}
func TransformKlogLine2ZeroLog(line string, zlog *zerolog.Logger) {
	parsed := reKlogParser.FindStringSubmatch(line)
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
		}
		msg := parsed[10]
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
		mye.Any("klog", kll).Msg(msg)
	} else {
		zlog.Warn().Str("klog", line).Msg("klog could not parsed")
	}
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

		for lineScanner.Scan() {
			line := lineScanner.Text()
			TransformKlogLine2ZeroLog(line, zlog)
		}
	}()
	return ret
}

func (kz Klog2zeroLog) Write(in []byte) (int, error) {
	return kz.piW.Write(in)
}
