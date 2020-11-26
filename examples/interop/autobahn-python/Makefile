PYENV := pyenv
PIP3 = $(PYENV)/bin/pip3

.PHONY: autobahn clean

all: autobahn

# Install autobahn dependencies into venv
# See: https://github.com/crossbario/autobahn-python/wiki/Autobahn-on-Ubuntu
autobahn: $(PIP3)
	$(PIP3) install autobahn[twisted,asyncio,serialization] > /dev/null
	@echo "===> installed autobahn"

# Install pip into venv
$(PIP3): $(PYENV) get-pip.py
	$(PYENV)/bin/python3 get-pip.py > /dev/null
	@echo "===> installed pip into venv"

# Create python venv
$(PYENV):
	python3 -m venv --without-pip $(PYENV)
	@echo "===> created venv $@"

# Get pip installer
get-pip.py:
	wget -N https://bootstrap.pypa.io/get-pip.py

# Remove venv and pip installer
clean:
	@rm -rf $(PYENV)
	@rm -f get-pip.py
