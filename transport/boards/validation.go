package boards

import (
	"net/url"
	"regexp"
	"strings"
)

const packetObjectStyle = "text;html=1;strokeColor=none;fillColor=none;align=left;verticalAlign=middle;whiteSpace=wrap;rounded=0;fontSize=1;"

var boardHash = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 512 || u.Scheme != "https" || u.Host != boardsBase || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Path != "/whiteboard/" && u.Path != "/guest/" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	return err == nil && len(q["hash"]) == 1 && boardHash.MatchString(q.Get("hash"))
}

func validSocketHost(host string) bool {
	return strings.HasSuffix(host, ".boards.yandex.ru") && !strings.ContainsAny(host, "/:@?#\\") && len(host) <= 253
}
