PLUGIN_NAME ?= glabservices/plugin-sshfs
PLUGIN_TAG ?= latest
DOCKER ?= docker

all: clean rootfs create

clean:
	@echo "### rm ./plugin"
	@rm -rf ./plugin

rootfs:
	@echo "### docker build: rootfs image with docker-volume-sshfs"
	@$(DOCKER) build -t ${PLUGIN_NAME}:rootfs .
	@echo "### create rootfs directory in ./plugin/rootfs"
	@mkdir -p ./plugin/rootfs
	@$(DOCKER) create --name tmp ${PLUGIN_NAME}:rootfs
	@$(DOCKER) export tmp | tar -x -C ./plugin/rootfs
	@echo "### copy config.json to ./plugin/"
	@cp config.json ./plugin/
	@$(DOCKER) rm -vf tmp

create:
	@echo "### remove existing plugin ${PLUGIN_NAME}:${PLUGIN_TAG} if exists"
	@$(DOCKER) plugin rm -f ${PLUGIN_NAME}:${PLUGIN_TAG} || true
	@echo "### create new plugin ${PLUGIN_NAME}:${PLUGIN_TAG} from ./plugin"
	@$(DOCKER) plugin create ${PLUGIN_NAME}:${PLUGIN_TAG} ./plugin

enable:
	@echo "### enable plugin ${PLUGIN_NAME}:${PLUGIN_TAG}"
	@$(DOCKER) plugin enable ${PLUGIN_NAME}:${PLUGIN_TAG}

push: clean rootfs create
	@echo "### push plugin ${PLUGIN_NAME}:${PLUGIN_TAG}"
	@$(DOCKER) plugin push ${PLUGIN_NAME}:${PLUGIN_TAG}

install:
	@$(DOCKER) plugin disable ${PLUGIN_NAME}:${PLUGIN_TAG}
	@$(DOCKER) plugin remove ${PLUGIN_NAME}:${PLUGIN_TAG}
	@$(DOCKER) plugin install ${PLUGIN_NAME}:${PLUGIN_TAG}
	@$(DOCKER) plugin enable ${PLUGIN_NAME}:${PLUGIN_TAG}
