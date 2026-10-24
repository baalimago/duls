# Records img/duls-demo.gif: three uses of duls on the fixture notebook in
# img/duls-demo/notes. Run from the repository root:
#   bash img/duls-demo/record.sh
# Where Chrome cannot use its sandbox, prefix the script with
# VHS_NO_SANDBOX=true.
#
# The window is 1400 px at font size 18: 114 columns, so the view is narrower
# than its 150-column reference and the peek column truncates. That is a wide
# terminal's own answer, not a recording artefact.
#
# Every act starts from an empty line-count store, so no act carries the
# "cache reused" footer and the view stays the subject of the recording.
Output img/duls-demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1400
Set Height 500
Set Padding 20
Set Theme "Catppuccin Mocha"
Set Framerate 10
Set TypingSpeed 40ms
Set CursorBlink false

Hide
Type "bash img/duls-demo/setup.sh"
Enter
Type "export HOME=$HOME/.cache/duls-demo/home"
Enter
Type "export PATH=$PWD/.build:$PATH"
Enter
Type "cd $HOME/notes"
Enter
Type "export PS1='~/notes $ '"
Enter
Type "clear"
Enter
Sleep 400ms
Show

Hide
Type "bash /tmp/duls-note.sh '1 · a directory as one size view'"
Enter
Sleep 300ms
Show
Sleep 1s

Type "duls"
Enter
Sleep 3s

Hide
Type "rm -rf $HOME/.cache/duls"
Enter
Type "clear"
Enter
Sleep 400ms
Type "bash /tmp/duls-note.sh '2 · --all keeps hidden entries'"
Enter
Sleep 300ms
Show
Sleep 1s

Type "duls --all"
Enter
Sleep 3s

Hide
Type "rm -rf $HOME/.cache/duls"
Enter
Type "clear"
Enter
Sleep 400ms
Type "bash /tmp/duls-note.sh '3 · a pipe gets the JSON document'"
Enter
Sleep 300ms
Show
Sleep 1s

Type "duls | jq '{files: .totals.files, bytes: .totals.bytes}'"
Enter
Sleep 4s
