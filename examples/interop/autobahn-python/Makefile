PYENV := pyenv
PIP3 = $(PYENV)/bin/pip3

.PHONY: autobahn clean

all: autobahn

autobahn: $(PIP3)
	$(PIP3) install autobahn[twisted] > /dev/null
	@echo "===> installed autobahn"

$(PIP3): $(PYENV) get-pip.py
	$(PYENV)/bin/python3 get-pip.py > /dev/null
	@echo "===> installed pip into venv"

$(PYENV):
	python3 -m venv --without-pip $(PYENV)
	@echo "===> created venv $@"

get-pip.py:
	wget -N https://bootstrap.pypa.io/get-pip.py

clean:
	@rm -rf $(PYENV)
	@rm -f get-pip.py
