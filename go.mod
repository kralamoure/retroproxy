module github.com/kralamoure/d1sniff

go 1.14

require (
	github.com/go-ozzo/ozzo-validation/v4 v4.2.2 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/kralamoure/d1 v0.0.0-20200814082459-874f984ec187 // indirect
	github.com/kralamoure/d1proto v0.0.0-20200713235525-ee4dfe007020
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.15.0
	golang.org/x/sys v0.0.0-20200814200057-3d37ad5750ed // indirect
)

replace github.com/kralamoure/d1proto => ../d1proto
