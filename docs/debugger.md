# Interpreter debugger

`kry debug FILE.kry [ARGS...]` checks the program and starts it in the
interpreter, pausing before its first statement. The debugger reads commands
from standard input; program output remains on standard output. It also accepts
`.kexe` artifacts and passes arguments through to `main`.

```text
break 18
break src/helper.kry:42
breakpoints
continue
locals
print request
stack
step
next
finish
clear 1
quit
```

`break LINE` uses the current source file. `break FILE:LINE` adds a breakpoint
in any loaded source. Breakpoints stop before the matching statement, including
repeated loop statements. `step` enters function calls, `next` runs through
calls at the current depth, and `finish` runs until the current function
returns. `locals` shows visible bindings from the current lexical scopes;
`print NAME` displays one binding. `stack` prints the current call stack.

The debugger instruments interpreter statement execution. Native executables
do not contain this runtime debugger.
