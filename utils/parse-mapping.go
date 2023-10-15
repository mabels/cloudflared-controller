package utils

import (
	"strconv"
	"strings"

	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
)

var validSchema = []string{"http", "https", "https-notlsverify"}

// AccessGroupEmail
// AccessGroupEmailDomain
// AccessGroupIP
// AccessGroupGeo
// AccessGroupServiceToken
// AccessGroupAnyValidServiceToken
// AccessGroupAccessGroup
// AccessGroupCertificate
// AccessGroupCertificateCommonName
// AccessGroupExternalEvaluation
// AccessGroupGSuite
// AccessGroupGitHub
// AccessGroupAzure
// AccessGroupOkta
// AccessGroupSAML
// AccessGroupAzureAuthContext
// AccessGroupAuthMethod
// AccessGroupLoginMethod
// AccessGroupDevicePosture
// AccessGroupDevicePosture
// AccessGroupIPList
// AccessGroupDetailResponse

func isValidSchema(schema string) *string {
	for _, s := range validSchema {
		if s == schema {
			return &s
		}
	}
	return nil
}

func ParseSvcMapping(log *zerolog.Logger, mapping string) []types.SvcAnnotationMapping {
	splitted := strings.Split(mapping, ",")
	for i, m := range splitted {
		splitted[i] = strings.TrimSpace(m)
	}
	ret := []types.SvcAnnotationMapping{}
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

		amap := types.SvcAnnotationMapping{}
		if len(splitted) > 0 && splitted[0] != "" {
			amap.PortName = splitted[0]
		} else {
			log.Warn().Str("splitting", m).Msg("Invalid mapping")
			continue
		}
		if len(splitted) > 1 && splitted[1] != "" {
			s := isValidSchema(splitted[1])
			if s == nil {
				log.Warn().Str("splitting", m).Msg("Invalid schema")
				continue
			}
			amap.Schema = *s
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

func ParseClassIngressMapping(log *zerolog.Logger, mapping string) []types.ClassIngressAnnotationMapping {
	splitted := strings.Split(mapping, ",")
	for i, m := range splitted {
		splitted[i] = strings.TrimSpace(m)
	}
	ret := []types.ClassIngressAnnotationMapping{}
	// hostname/schema[/hostheader]|path,
	for _, m := range splitted {
		splitted := strings.SplitN(m, "|", 2)
		if len(splitted) == 0 {
			log.Warn().Str("we need a path", m).Msg("Invalid mapping")
			continue
		}
		iam := types.ClassIngressAnnotationMapping{
			Schema: "http",
			Path:   "/",
		}
		if len(splitted) == 2 {
			iam.Path = splitted[1]
		}
		splitted = strings.SplitN(splitted[0], "/", 3)
		if len(splitted) == 0 {
			log.Warn().Str("we need a host", m).Msg("Invalid mapping")
			continue
		}
		if len(splitted) >= 1 {
			if splitted[0] == "" {
				log.Warn().Str("we need a host", m).Msg("Invalid mapping")
				continue
			}
			iam.Hostname = splitted[0]
		}
		if len(splitted) >= 2 {
			if splitted[1] != "" {
				s := isValidSchema(splitted[1])
				if s == nil {
					log.Warn().Str("we need a valid schema", m).Msg("Invalid mapping")
					continue
				}
				iam.Schema = splitted[1]
			}

		}
		if len(splitted) >= 3 {
			if splitted[2] != "" {
				iam.HostHeader = &splitted[2]
			}
		}
		ret = append(ret, iam)
	}
	return ret
}

func ParseStackIngressMapping(log *zerolog.Logger, mapping string) []types.StackIngressAnnotationMapping {
	splitted := strings.Split(mapping, ",")
	for i, m := range splitted {
		splitted[i] = strings.TrimSpace(m)
	}
	ret := []types.StackIngressAnnotationMapping{}
	// schema/hostname/int-port/hostheader/ext-host|path,
	for _, m := range splitted {
		splitted := strings.SplitN(m, "|", 2)
		if len(splitted) == 0 {
			log.Warn().Str("we need a path", m).Msg("Invalid mapping")
			continue
		}
		iam := types.StackIngressAnnotationMapping{
			InternPort: 80,
			Schema:     "http",
			Path:       "/",
		}
		if len(splitted) == 2 {
			iam.Path = splitted[1]
		}
		splitted = strings.SplitN(splitted[0], "/", 5)
		if len(splitted) == 0 {
			log.Warn().Str("we need a host", m).Msg("Invalid mapping")
			continue
		}
		if len(splitted) >= 1 {
			if splitted[0] != "" {
				s := isValidSchema(splitted[0])
				if s == nil {
					log.Warn().Str("we need a valid schema", m).Msg("Invalid mapping")
					continue
				}
				iam.Schema = splitted[0]
			}
		}
		if len(splitted) >= 2 {
			if splitted[1] == "" {
				log.Warn().Str("we need a host", m).Msg("Invalid mapping")
				continue
			}
			iam.Hostname = splitted[1]

		}
		if len(splitted) >= 3 {
			if splitted[2] != "" {
				nr, err := strconv.ParseInt(splitted[2], 10, 16)
				if err != nil || !(0 < nr && nr < 0x10000) {
					log.Warn().Str("we need a port", m).Msg("Invalid mapping")
					continue
				}
				iam.InternPort = int(nr)
			}
		}
		if len(splitted) >= 4 {
			if splitted[3] != "" {
				iam.HostHeader = &splitted[3]
			}
		}
		if len(splitted) < 5 {
			log.Warn().Str("we need a ext-host", m).Msg("Invalid mapping")
			continue
		} else {
			if splitted[4] == "" {
				log.Warn().Str("we need a ext-host", m).Msg("Invalid mapping")
				continue
			}
			iam.ExtHostName = splitted[4]
		}
		ret = append(ret, iam)
	}
	return ret
}
