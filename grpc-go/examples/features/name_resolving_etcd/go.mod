module name_resolving_etcd

go 1.16

//replace github.com/coreos/bbolt v1.3.6 => go.etcd.io/bbolt v1.3.6
replace google.golang.org/grpc => google.golang.org/grpc v1.26.0

// go.etcd.io/bbolt@v1.3.6 used for two different module paths (github.com/coreos/bbolt and go.etcd.io/
// 报上面错误，则添加下面两行代码
replace github.com/coreos/bbolt v1.3.4 => go.etcd.io/bbolt v1.3.4
replace go.etcd.io/bbolt v1.3.4 => github.com/coreos/bbolt v1.3.4

require (
	github.com/coreos/bbolt v1.3.4 // indirect
	github.com/coreos/etcd v3.3.25+incompatible
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20201229170055-e5319fda7802 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.etcd.io/bbolt v1.3.4 // indirect
	go.etcd.io/etcd/client/v3 v3.5.0
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac // indirect
	google.golang.org/grpc v1.38.0
)