PLB=/usr/libexec/PlistBuddy
name=$(shell $(PLB) -c Print:name src/info.plist)

all: build open

open: bin/$(name).alfredworkflow
	open bin/$(name).alfredworkflow

build:
	-rm bin/*.alfredworkflow
	-pushd src; v=$$(git describe --tags); $(PLB) -c "Set:version $${v#v}" info.plist; popd
	-pushd src/alchromepass.d; GOOS=darwin GOARCH=amd64 go build -o ../alchromepass main.go; popd
	-pushd src; zip -r ../bin/$(name).alfredworkflow alchromepass icon*.png info.plist; popd


update: build
	cp -f src/alchromepass '/Users/mnaito/Library/Mobile Documents/com~apple~CloudDocs/Alfred/Alfred.alfredpreferences/workflows/user.workflow.8221232E-CD36-41A3-9539-B1127FE8A67A/'
	cp -f '/Users/mnaito/Library/Mobile Documents/com~apple~CloudDocs/Alfred/Alfred.alfredpreferences/workflows/user.workflow.8221232E-CD36-41A3-9539-B1127FE8A67A/info.plist' src/	