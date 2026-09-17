package transport

import "testing"

func TestProxySelectorForURLAndTunnelHelpers(t *testing.T) {
	clearProxyEnvironment(t)
	sel := newProxySelector(httpSettings{proxy: "http://proxy.local:3128", proxySet: true})
	if u, err := sel.forURL("http://\x00"); u != nil || err == nil {
		t.Fatalf("forURL on a bad url = %v, %v; want an error", u, err)
	}
	if u, err := sel.forURL("https://git.example.com/repo.git"); err != nil || u == nil {
		t.Fatalf("forURL = %v, %v; want the proxy", u, err)
	}

	socks := newProxySelector(httpSettings{proxy: "socks5://proxy.local:1080", proxySet: true})
	if u, err := socks.forURL("https://git.example.com/repo.git"); u != nil || err != nil {
		t.Fatalf("forURL through socks = %v, %v; want no HTTP proxy", u, err)
	}

	tlsProxy := newProxySelector(httpSettings{proxy: "https://secure.proxy:8443", proxySet: true})
	if !tlsProxy.isTLSProxyAddress("secure.proxy:8443") {
		t.Fatalf("isTLSProxyAddress should match the configured https proxy")
	}
	if u, err := tlsProxy.tunnelFor("secure.proxy:8443"); u != nil || err != nil {
		t.Fatalf("tunnelFor its own https proxy = %v, %v; want no nested tunnel", u, err)
	}
	if u, err := tlsProxy.tunnelFor("git.example.com:443"); err != nil || u == nil {
		t.Fatalf("tunnelFor a target = %v, %v; want the proxy", u, err)
	}
}
