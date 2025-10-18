# Fake sample plugins used for testing
PLUGINS := fake_test fake_test_as multi_plugin fake_init_service unauthorized_plugin

all: $(PLUGINS:%=%/plugin.wasm)

clean:
	rm -f $(PLUGINS:%=%/plugin.wasm)

%/plugin.wasm: %/plugin.go
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o $@ ./$*

%/plugin.wasm: %/plugin.ts
	cd $(@D) && npm run build