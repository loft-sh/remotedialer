.PHONY: run-dummy
run-dummy:
	go run dummy/main.go --listen=localhost:8125

ID := "1"
TOKEN := "123"
PEERS := ""

.PHONY: run-server
run-server:
	go run server/main.go --id=$(ID) --token=$(TOKEN) --peers=$(PEERS) --listen=localhost:8123

.PHONY: run-client
run-client:
	go run client/main.go --id=client-id --debug