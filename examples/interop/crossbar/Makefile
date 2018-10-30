PYENV := pyenv
PIP3 = $(PYENV)/bin/pip3
CROSSBAR = $(PYENV)/bin/crossbar

.PHONY: all clean crossbar nodeclean pip3

all: crossbar

crossbar: node
	@echo "Configure crossbar node: edit node/.crossbar/config.json"
	@echo "Start crossbar node: $(CROSSBAR) start --cbdir node/.crossbar"

pip3: $(PIP3)

node: $(CROSSBAR)
	$(CROSSBAR) init --appdir node > /dev/null
	@echo "===> Initialized crossbar node"

$(CROSSBAR): $(PIP3)
	@echo "===> installing $@"
	@$(PIP3) install -U crossbar > /dev/null
	@touch $(CROSSBAR)
	@echo "===> Created python3 virtualenv with crossbar"

$(PIP3): $(PYENV) get-pip.py
	$(PYENV)/bin/python3 get-pip.py > /dev/null
	@echo "===> installed pip into venv"

$(PYENV):
	python3 -m venv --without-pip $(PYENV)
	@echo "===> created venv $@"

get-pip.py:
	wget -N https://bootstrap.pypa.io/get-pip.py

nodeclean:
	@rm -rf node

clean: nodeclean
	@rm -f get-pip.py
	@rm -rf $(PYENV)
