package transport

import "strings"

const (
	CapMultiAck         = "multi_ack"
	CapMultiAckDetailed = "multi_ack_detailed"
	CapSideBand         = "side-band"
	CapSideBand64k      = "side-band-64k"
	CapThinPack         = "thin-pack"
	CapOfsDelta         = "ofs-delta"
	CapShallow          = "shallow"
	CapDeepenSince      = "deepen-since"
	CapDeepenNot        = "deepen-not"
	CapNoProgress       = "no-progress"
	CapIncludeTag       = "include-tag"
	CapReportStatus     = "report-status"
	CapReportStatusV2   = "report-status-v2"
	CapDeleteRefs       = "delete-refs"
	CapAtomic           = "atomic"
	CapPushOptions      = "push-options"
	CapAgent            = "agent"
	CapObjectFormat     = "object-format"
	CapSymref           = "symref"
	CapFilter           = "filter"
)

type Capabilities struct {
	values map[string][]string
	order  []string
}

func ParseCapabilities(line string) Capabilities {
	var parsed Capabilities
	for _, token := range strings.Fields(line) {
		parsed.Add(token)
	}
	return parsed
}

func (c *Capabilities) Add(token string) {
	name, value, hasValue := strings.Cut(token, "=")
	if name == "" {
		return
	}
	if c.values == nil {
		c.values = make(map[string][]string)
	}
	if _, exists := c.values[name]; !exists {
		c.order = append(c.order, name)
		c.values[name] = []string{}
	}
	if hasValue {
		c.values[name] = append(c.values[name], value)
	}
}

func (c Capabilities) Has(name string) bool {
	_, ok := c.values[name]
	return ok
}

func (c Capabilities) Value(name string) (string, bool) {
	values, ok := c.values[name]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

func (c Capabilities) Values(name string) []string {
	return append([]string(nil), c.values[name]...)
}

func (c Capabilities) Names() []string {
	return append([]string(nil), c.order...)
}

func (c Capabilities) Intersect(other Capabilities) Capabilities {
	var result Capabilities
	for _, name := range c.order {
		if !other.Has(name) {
			continue
		}
		if result.values == nil {
			result.values = make(map[string][]string)
		}
		result.order = append(result.order, name)
		result.values[name] = append([]string(nil), c.values[name]...)
	}
	return result
}

func (c Capabilities) String() string {
	var tokens []string
	for _, name := range c.order {
		values := c.values[name]
		if len(values) == 0 {
			tokens = append(tokens, name)
			continue
		}
		for _, value := range values {
			tokens = append(tokens, name+"="+value)
		}
	}
	return strings.Join(tokens, " ")
}
