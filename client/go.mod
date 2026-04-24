module github.com/hrygo/hotplex/client

go 1.26

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
)

require github.com/hrygo/hotplex v0.0.0

replace github.com/hrygo/hotplex => ..
