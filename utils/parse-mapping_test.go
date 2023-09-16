package utils

import (
	"os"
	"testing"

	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestParseMapping(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseSvcMapping(&log, "")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{})

}

func TestParseMappingPortName(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseSvcMapping(&log, "hallo")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, "//,hallo,/")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

}

func TestParseMappingPortSchema(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()

	mapping := ParseSvcMapping(&log, "hallo/xxxx")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{})

	mapping = ParseSvcMapping(&log, "hallo/http")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, "xallo/xxxx/,hallo/http,murks/https")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	},
		{
			PortName: "murks",
			Schema:   "https",
			Path:     "/",
			Order:    1,
		},
	})
}

func TestParseMappingNextJS(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	mapping := ParseSvcMapping(&log, "public/http|^\\/(assets|favicon|fonts|logo)\\/,public/http|^\\/manifest.json$,next/http/")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{
		{
			PortName: "public",
			Schema:   "http",
			Path:     "^\\/(assets|favicon|fonts|logo)\\/",
			Order:    0,
		},
		{
			PortName: "public",
			Schema:   "http",
			Path:     "^\\/manifest.json$",
			Order:    1,
		},
		{
			PortName: "next",
			Schema:   "http",
			Path:     "/",
			Order:    2,
		},
	})
}

func TestParseMappingPortSchemaPath(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	mapping := ParseSvcMapping(&log, "hallo/http/////")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
			Order:    0,
		},
	})

	mapping = ParseSvcMapping(&log, "hallo/http|||||||")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "||||||",
			Order:    0,
		},
	})
	mapping = ParseSvcMapping(&log, "hallo/http|")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "",
			Order:    0,
		},
	})

	mapping = ParseSvcMapping(&log, "hallo/http|meno/wurks")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "meno/wurks",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, "hallo/http|||||||/////meno")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "||||||/////meno",
		Order:    0,
	}})
	mapping = ParseSvcMapping(&log, "hallo/http||||/////meno/wurks")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "|||/////meno/wurks",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, "hallo/http/meno/wurks")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno/wurks",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, "hallo/http/////meno")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno",
		Order:    0,
	}})
	mapping = ParseSvcMapping(&log, "hallo/http/////meno/wurks")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/meno/wurks",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, "hallo/http")
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{{
		PortName: "hallo",
		Schema:   "http",
		Path:     "/",
		Order:    0,
	}})

	mapping = ParseSvcMapping(&log, `hallo/http/////  ,
		hallo/http|||/me/no ,
		hallo/http/meno ,
		hallo/http/////meno ,
		hallo/http,
		hallo/http/////meno/wurks,hallo/http/meno/wurks/`)
	assert.Equal(t, mapping, []types.SvcAnnotationMapping{
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
			Order:    0,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "||/me/no",
			Order:    1,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno",
			Order:    2,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno",
			Order:    3,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/",
			Order:    4,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno/wurks",
			Order:    5,
		},
		{
			PortName: "hallo",
			Schema:   "http",
			Path:     "/meno/wurks/",
			Order:    6,
		},
	})
}

func toPtr(s string) *string {
	return &s
}

func TestClassIngressSchema(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	// hostname/schema[/hostheader]|path,
	mapping := ParseClassIngressMapping(&log, `
		/  ,
		//  ,
		///|  ,
		|  ,
		/|,
		//|,
		///|,
		1hallo,
		2hallo|/doof|,
		3hallo/|/doof|,
		4hallo//|/doof|,
		/http/|/doof|,
		5hallo/bloed/|/doof|,
		6hallo/http/|/doof|,
		7hallo/https/|/doof|,
		8hallo/https/mmm/xx|/doof|,
		9hallo//mmm/xxx|/doof|,
		10hallo/https/mmm/xxxx|/doof|,
	`)
	assert.Equal(t, mapping, []types.ClassIngressAnnotationMapping{
		{Hostname: "1hallo", Schema: "http", HostHeader: nil, Path: "/"},
		{Hostname: "2hallo", Schema: "http", HostHeader: nil, Path: "/doof|"},
		{Hostname: "3hallo", Schema: "http", HostHeader: nil, Path: "/doof|"},
		{Hostname: "4hallo", Schema: "http", HostHeader: nil, Path: "/doof|"},
		{Hostname: "6hallo", Schema: "http", HostHeader: nil, Path: "/doof|"},
		{Hostname: "7hallo", Schema: "https", HostHeader: nil, Path: "/doof|"},
		{Hostname: "8hallo", Schema: "https", HostHeader: toPtr("mmm/xx"), Path: "/doof|"},
		{Hostname: "9hallo", Schema: "http", HostHeader: toPtr("mmm/xxx"), Path: "/doof|"},
		{Hostname: "10hallo", Schema: "https", HostHeader: toPtr("mmm/xxxx"), Path: "/doof|"},
	})

}

func TestStackIngressSchema(t *testing.T) {
	log := zerolog.New(os.Stdout).With().Logger()
	// schema/hostname/int-port/hostheader/ext-host|path,
	mapping := ParseStackIngressMapping(&log, `
		/  ,
		//  ,
		///|  ,
		////|  ,
		/////|  ,
		http,
		http/a,
		http/a/0,
		http/a/0/c,
		http/a/0/c/d,
		http/a/0/c/d/e,
		http/a/0/c/d/e|/meno,

		http/aa/,
		http/ab//c,
		http/ac//c/d,
		http/ad//c/d/e,
		http/ae//c/d/e|/meno,
		http/af/c/d/e|/meno,

		http/aaa/27,
		http/aab/27/c,
		http/aac/27/c/d,
		http/aac1/27/c/d/,
		http/aac2/27/c/d/|/meno|,
		http/aad/27/c/d/e,
		http/aae/27/c/d/e|/meno,
		http/aaf/27/c/d/e|/meno|,

		http/aba/28,
		http/abb/28/c,
		http/abc/28/c,
		http/abd/28/c/|/meno,
		http/abe/28/c/|/meno|,
		http/abf/28/c/d|/meno,
		http/abg/28/c/d|/meno|,

	`)
	assert.Equal(t, mapping, []types.StackIngressAnnotationMapping{
		{Hostname: "ac", Schema: "http", InternPort: 80, HostHeader: toPtr("c"), ExtHostName: "d", Path: "/"},
		{Hostname: "ad", Schema: "http", InternPort: 80, HostHeader: toPtr("c"), ExtHostName: "d/e", Path: "/"},
		{Hostname: "ae", Schema: "http", InternPort: 80, HostHeader: toPtr("c"), ExtHostName: "d/e", Path: "/meno"},
		{Hostname: "aac", Schema: "http", InternPort: 27, HostHeader: toPtr("c"), ExtHostName: "d", Path: "/"},
		{Hostname: "aac1", Schema: "http", InternPort: 27, HostHeader: toPtr("c"), ExtHostName: "d/", Path: "/"},
		{Hostname: "aac2", Schema: "http", InternPort: 27, HostHeader: toPtr("c"), ExtHostName: "d/", Path: "/meno|"},
		{Hostname: "aad", Schema: "http", InternPort: 27, HostHeader: toPtr("c"), ExtHostName: "d/e", Path: "/"},
		{Hostname: "aae", Schema: "http", InternPort: 27, HostHeader: toPtr("c"), ExtHostName: "d/e", Path: "/meno"},
		{Hostname: "aaf", Schema: "http", InternPort: 27, HostHeader: toPtr("c"), ExtHostName: "d/e", Path: "/meno|"},
		{Hostname: "abf", Schema: "http", InternPort: 28, HostHeader: toPtr("c"), ExtHostName: "d", Path: "/meno"},
		{Hostname: "abg", Schema: "http", InternPort: 28, HostHeader: toPtr("c"), ExtHostName: "d", Path: "/meno|"},
	})

}
