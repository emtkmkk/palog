module github.com/miscord-dev/palog

go 1.21.6

require (
	github.com/gorcon/rcon v1.3.4
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58
)

replace (
    github.com/pbnjay/memory => github.com/dustin-decker/memory v0.0.0-20220311051549-ea268d315863
)