module github.com/kralamoure/d1sniff

go 1.14

require (
	github.com/asaskevich/govalidator v0.0.0-20200819183940-29e1ff8eb0bb // indirect
	github.com/go-ozzo/ozzo-validation/v4 v4.2.2 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/kralamoure/d1 v0.0.0-20200822061306-54fdafbf2078 // indirect
	github.com/kralamoure/d1proto v0.0.0-20200713235525-ee4dfe007020
	github.com/spf13/pflag v1.0.5
	go.uber.org/zap v1.15.0
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a // indirect
	golang.org/x/sys v0.0.0-20200826173525-f9321e4c35a6 // indirect
)

replace github.com/kralamoure/d1 => ../d1

replace github.com/kralamoure/d1proto => ../d1proto
