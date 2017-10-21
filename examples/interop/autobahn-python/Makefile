PY_ENV := pyenv1

.PHONY: pyenv clean

pyenv: $(PY_ENV)

$(PY_ENV):
	virtualenv $(PY_ENV)
	$(PY_ENV)/bin/pip install -U setuptools
	$(PY_ENV)/bin/pip install autobahn[twisted]
	@echo "Created python virtualenv"

clean:
	rm -rf $(PY_ENV)
