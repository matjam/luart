# Lua 5.5.1 test suite

The official test suite for Lua 5.5.1, unmodified, from
<https://www.lua.org/tests/lua-5.5.1-tests.tar.gz> (SHA-256
`da07b543872dc0bb2ff12aabd0c248578d78df3eb6b67efdc537a46d455c7f31`).
Copyright © 1994–2025 Lua.org, PUC-Rio, under the MIT licence; see the
notice in each file.

`TestLua55` in `lua/lua55_test.go` runs it as `all.lua` does. Files luart
does not pass yet are listed there as pending, with the reason; changes
to luart shrink that list rather than editing these files.
