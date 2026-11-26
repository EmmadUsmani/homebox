import os

from packaging.version import Version

assert os.geteuid() > 0
assert Version("42.0rc1") < Version("42.0")
print("python:pure:42")
