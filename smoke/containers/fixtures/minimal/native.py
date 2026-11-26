from markupsafe._speedups import _escape_inner

import pure

assert _escape_inner("<42>") == "&lt;42&gt;"
print("python:native:42")
