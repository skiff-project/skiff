# Thin wrapper around build.sh (which also works without make).
.PHONY: all build portable release dev test itest uitest ui
all: build
build portable release dev test itest uitest ui:
	./build.sh $@
