module github.com/kralamoure/d1sniff

go 1.15

require (
	github.com/go-ozzo/ozzo-validation/v4 v4.2.2 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/kralamoure/d1 v0.0.0-20200917030335-f23076eacc5c // indirect
	github.com/kralamoure/d1proto v0.0.0-20200713235525-ee4dfe007020
	github.com/kralamoure/dofus v0.0.0-20200917024449-5e4b76236af8 // indirect
	github.com/spf13/pflag v1.0.5
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.16.0
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a // indirect
	golang.org/x/sys v0.0.0-20200916084744-dbad9cb7cb7a // indirect
)

replace github.com/kralamoure/d1 => ../d1

replace github.com/kralamoure/d1proto => ../d1proto
