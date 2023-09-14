package utils

import (
	"strings"

	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
)

var validSchema = []string{"http", "https", "https-notlsverify"}

func ParseMapping(log *zerolog.Logger, mapping string) []types.AnnotationMapping {
	splitted := strings.Split(mapping, ",")
	for i, m := range splitted {
		splitted[i] = strings.TrimSpace(m)
	}
	ret := []types.AnnotationMapping{}
	for _, m := range splitted {
		splitted := strings.SplitN(m, "/", 2)
		pipeOrSlash := ""
		if len(splitted) == 2 {
			last := splitted[len(splitted)-1]
			idxSlash := strings.Index(last, "/")
			if idxSlash < 0 {
				idxSlash = 0x8000
			}
			idxPipe := strings.Index(last, "|")
			if idxPipe < 0 {
				idxPipe = 0x8000
			}
			if idxSlash > idxPipe {
				splitPipe := strings.SplitN(last, "|", 2)
				if len(splitPipe) > 1 {
					splitted[len(splitted)-1] = splitPipe[0]
					splitted = append(splitted, splitPipe[1])
					pipeOrSlash = "|"
				}
			} else if idxPipe > idxSlash {
				splitSlash := strings.SplitN(last, "/", 2)
				if len(splitSlash) > 1 {
					splitted[len(splitted)-1] = splitSlash[0]
					splitted = append(splitted, splitSlash[1])
					pipeOrSlash = "/"
				}
			}
		}

		amap := types.AnnotationMapping{}
		if len(splitted) > 0 && splitted[0] != "" {
			amap.PortName = splitted[0]
		} else {
			log.Warn().Str("splitting", m).Msg("Invalid mapping")
			continue
		}
		if len(splitted) > 1 && splitted[1] != "" {
			for _, s := range validSchema {
				if s == splitted[1] {
					amap.Schema = splitted[1]
				}
			}
			if amap.Schema == "" {
				log.Warn().Str("splitting", m).Msg("Invalid schema")
				continue
			}
		} else {
			amap.Schema = "http"
		}
		if len(splitted) > 2 {
			switch pipeOrSlash {
			case "|":
				amap.Path = splitted[2]
			case "/":
				amap.Path = "/" + strings.TrimLeft(splitted[2], "/")
			default:
				amap.Path = "/"
			}
		} else {
			amap.Path = "/"
		}
		amap.Order = len(ret)
		ret = append(ret, amap)
	}
	return ret
}
