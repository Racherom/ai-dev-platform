module github.com/racherom/ai-dev-platform/services/runner

go 1.26.8

toolchain go1.27.3

require (
	github.com/jackc/pgx/v5 v5.12.0
	github.com/racherom/modular-platform/sdk v0.0.0-20261120145312-1eea5a896d43
	sigs.k8s.io/yaml v1.6.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)

tool github.com/racherom/modular-platform/sdk/cmd/sdkgen
