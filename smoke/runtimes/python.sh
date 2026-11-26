#!/usr/bin/env bash
# shellcheck source=smoke/runtimes/fixture.sh
source ./fixture.sh

pip_app() {
    python -m venv .venv
    .venv/bin/python -m pip install --only-binary=:all: numpy==2.4.3 pandas==3.0.1 pytest==9.0.2
    cat > test_frame.py <<'PY'
import numpy as np
import pandas as pd

def test_frame():
    assert pd.Series(np.array([20, 22])).sum() == 42
PY
    .venv/bin/python -m pytest -q
}

uv_app() {
    uv init --bare --name homebox-smoke --python 3.14
    uv add 'click==8.3.1'
    uv sync --locked
    uv run --locked python -c 'import click; assert click.unstyle(click.style("smoke", fg="green")) == "smoke"'
    uv python install 3.13
    uv run --isolated --no-project --managed-python --python 3.13 python -c 'import sys; assert sys.version_info[:2] == (3, 13)'
}

tools() {
    uvx --from cowsay==6.1 cowsay -t homebox-uvx | grep -F homebox-uvx
    pipx run --spec cowsay==6.1 cowsay -t homebox-pipx | grep -F homebox-pipx
}

native_extension() {
    cat > setup.py <<'PY'
from setuptools import Extension, setup

setup(name="homebox-native", version="0.1", ext_modules=[Extension("answer", ["answer.c"])])
PY
    cat > answer.c <<'C'
#include <Python.h>

static PyObject *answer(PyObject *self, PyObject *args) {
    return PyLong_FromLong(42);
}

static PyMethodDef methods[] = {
    {"value", answer, METH_NOARGS, NULL},
    {NULL, NULL, 0, NULL}
};

static struct PyModuleDef module = {
    PyModuleDef_HEAD_INIT, "answer", NULL, -1, methods
};

PyMODINIT_FUNC PyInit_answer(void) {
    return PyModule_Create(&module);
}
C
    python -m venv .venv
    .venv/bin/python -m pip install .
    .venv/bin/python -c 'import answer; assert answer.value() == 42'
}

fastapi_app() {
    python -m venv .venv
    uv pip install --python .venv/bin/python 'fastapi==0.128.0' 'uvicorn==0.40.0'
    cat > app.py <<'PY'
from fastapi import FastAPI

app = FastAPI()

@app.get("/")
def home():
    return {"message": "homebox-fastapi"}
PY
    expect_http / '{"message":"homebox-fastapi"}' .venv/bin/uvicorn app:app --host 127.0.0.1 --port 18765
}

django_app() {
    python -m venv .venv
    uv pip install --python .venv/bin/python 'django==6.0.2'
    .venv/bin/django-admin startproject smoke_site .
    .venv/bin/python manage.py migrate --noinput
    .venv/bin/python manage.py shell -c 'from django.contrib.contenttypes.models import ContentType; assert ContentType.objects.count() > 0'
    cat > smoke_site/urls.py <<'PY'
from django.http import HttpResponse
from django.urls import path

def home(request):
    return HttpResponse("homebox-django")

urlpatterns = [path("", home)]
PY
    expect_http / homebox-django .venv/bin/python manage.py runserver --noreload 127.0.0.1:18765
}

notebook() {
    python -m venv .venv
    uv pip install --python .venv/bin/python 'nbconvert==7.17.0' 'ipykernel==7.1.0'
    cat > smoke.ipynb <<'JSON'
{
  "cells": [{"cell_type": "code", "id": "smoke", "execution_count": null,
    "metadata": {}, "outputs": [],
    "source": ["print('homebox-result:', 6 * 7)"]}],
  "metadata": {"kernelspec": {"display_name": "Python 3", "language": "python", "name": "python3"}},
  "nbformat": 4, "nbformat_minor": 5
}
JSON
    PATH="$PWD/.venv/bin:$PATH" jupyter nbconvert --to notebook --execute \
        --ExecutePreprocessor.timeout=60 --output executed.ipynb smoke.ipynb
    python - <<'PY'
import json
with open("executed.ipynb") as notebook:
    output = json.load(notebook)["cells"][0]["outputs"][0]["text"]
assert "".join(output).strip() == "homebox-result: 42"
PY
}

"$1"
