# Records docs/demo.gif against the mock adapter, so it needs no account.
#   make build && vhs docs/demo.tape
Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 700
Set TypingSpeed 40ms

Type "./bin/alchemist --adapter mock --history=false"
Enter
Sleep 2s

# Open the first database and select its first container as the scope.
Enter
Sleep 500ms
Down
Enter
Sleep 1s

# Write a query and run it.
Type "e"
Type "SELECT TOP 10 * FROM c WHERE c.amount > 100"
Sleep 500ms
Ctrl+R
Sleep 2s

# Leaving the editor shows the query highlighted; fetch a second page.
Tab
Sleep 1s
Type "m"
Sleep 1500ms

# Look at one document, then export what has been fetched.
Enter
Sleep 2s
Escape
Ctrl+E
Sleep 1s
Ctrl+U
Type "/tmp/alchemist-demo.csv!"
Sleep 500ms
Enter
Sleep 2500ms

Type "q"
