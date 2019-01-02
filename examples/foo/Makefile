C_SRC=$(wildcard *.c)
LL_SRC=$(C_SRC:.c=.ll)
LL_DBG_SRC=$(LL_SRC:.ll=_dbg.ll)
GO_SRC=$(LL_SRC:.ll=.go)

all: $(GO_SRC) $(LL_SRC) $(LL_DBG_SRC)

%.go: %.ll %_dbg.ll
	ll2dot2 -f -img $<
	restructure2 -steps -img -indent -o foo_graphs/main.json foo_graphs/main.dot
	explore -style monokai -f $<
	ll2go2 -o $@ $<
	#go-post $@
	#sar -i "\n\n" "\n" $@
	#goimports -w $@

%_dbg.ll: %.c
	clang -g -S -emit-llvm -o $@ $<

%.ll: %.c
	clang -S -emit-llvm -o $@ $<
	#sar -i '#[0-9]+ {' '{' $@
	#sar -i '}\n\nattributes(.|[\n])*' '}\n' $@
	# Optimize after removing optnone option.
	#opt -S --mem2reg -o $@ $@

.PHONY: clean

clean:
	rm -rf *.go *.ll *_graphs/ *_explore/
