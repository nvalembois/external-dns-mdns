module github.com/nvalembois/external-dns-mdns-server

go 1.25.0

require github.com/miekg/dns v1.1.72

require golang.org/x/net v0.57.0 // indirect

require golang.org/x/sys v0.47.0 // indirect

require (
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
)

replace golang.org/x/net => github.com/golang/net v0.27.0

replace golang.org/x/sys => github.com/golang/sys v0.22.0
