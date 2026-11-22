module github.com/racherom/ai-dev-platform/services/scheduler

go 1.26.8

toolchain go1.27.3

tool github.com/racherom/modular-platform/sdk/cmd/sdkgen

require (
	github.com/jackc/pgx/v5 v5.13.0
	github.com/racherom/ai-dev-platform/libs v0.0.0
	github.com/racherom/modular-platform/sdk v0.0.0-20261120145312-1eea5a896d43
)

require (
	github.com/go-jose/go-jose/v4 v4.1.7 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mfridman/interpolate v0.2.0 // indirect
	github.com/pressly/goose/v3 v3.30.1 // indirect
	github.com/riverqueue/river v0.51.2 // indirect
	github.com/riverqueue/river/riverdriver v0.51.2 // indirect
	github.com/riverqueue/river/riverdriver/riverpgxv5 v0.51.2 // indirect
	github.com/riverqueue/river/rivershared v0.51.2 // indirect
	github.com/riverqueue/river/rivertype v0.51.2 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace github.com/racherom/ai-dev-platform/libs => ../../libs
