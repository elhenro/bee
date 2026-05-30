#!/bin/sh
# run the test suite. works on Node 22+ (strip-types behind a flag) and 26+
# (native). use this instead of guessing node flags or looking for tsc/ts-node.
exec node --experimental-strip-types --test "test/*.test.ts"
