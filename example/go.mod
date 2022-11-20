module github.com/gogf/gf/example

go 1.15

require (
	github.com/gogf/gf/contrib/config/apollo/v2 v2.0.0
	github.com/gogf/gf/contrib/config/kubecm/v2 v2.0.0
	github.com/gogf/gf/contrib/config/nacos/v2 v2.2.2
	github.com/gogf/gf/contrib/config/polaris/v2 v2.0.0
	github.com/gogf/gf/contrib/drivers/mysql/v2 v2.0.0
	github.com/gogf/gf/contrib/registry/etcd/v2 v2.1.4
	github.com/gogf/gf/contrib/registry/polaris/v2 v2.0.0
	github.com/gogf/gf/contrib/rpc/grpcx/v2 v2.0.0
	github.com/gogf/gf/contrib/trace/jaeger/v2 v2.0.0
	github.com/gogf/gf/v2 v2.2.1
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/nacos-group/nacos-sdk-go v1.1.2
	github.com/polarismesh/polaris-go v1.2.0-beta.3
	golang.org/x/net v0.0.0-20220919232410-f2f64ebce3c1
	google.golang.org/grpc v1.49.0
	google.golang.org/protobuf v1.28.1
	k8s.io/client-go v0.25.2
)

replace (
	github.com/gogf/gf/contrib/config/apollo/v2 => ../contrib/config/apollo/
	github.com/gogf/gf/contrib/config/kubecm/v2 => ../contrib/config/kubecm/
	github.com/gogf/gf/contrib/config/polaris/v2 => ../contrib/config/polaris/
	github.com/gogf/gf/contrib/drivers/mysql/v2 => ../contrib/drivers/mysql/
	github.com/gogf/gf/contrib/registry/etcd/v2 => ../contrib/registry/etcd/
	github.com/gogf/gf/contrib/registry/polaris/v2 => ../contrib/registry/polaris/
	github.com/gogf/gf/contrib/rpc/grpcx/v2 => ../contrib/rpc/grpcx/
	github.com/gogf/gf/contrib/trace/jaeger/v2 => ../contrib/trace/jaeger/
	github.com/gogf/gf/v2 => ../
)
