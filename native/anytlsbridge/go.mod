module github.com/chocice1994/v2rayNG/native/anytlsbridge

go 1.23.1

require (
	github.com/anytls/sing-anytls v0.0.11
	github.com/sagernet/sing v0.6.1
)

require golang.org/x/sys v0.30.0 // indirect

replace github.com/anytls/sing-anytls => ../third_party/sing-anytls
