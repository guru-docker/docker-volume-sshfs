PLUGIN_NAME = pavhov/sshfs
PLUGIN_TAG ?= latest

all: clean rootfs create

clean:
	@echo "### rm ./plugin"
	@rm -rf ./plugin

rootfs:
	@echo "### docker build: rootfs image with docker-volume-sshfs"
	@docker --context=glab build -t ${PLUGIN_NAME}:rootfs .
	@echo "### create rootfs directory in ./plugin/rootfs"
	@mkdir -p ./plugin/rootfs
	@docker --context=glab create --name tmp ${PLUGIN_NAME}:rootfs
	@docker --context=glab export tmp | tar -x -C ./plugin/rootfs
	@echo "### copy config.json to ./plugin/"
	@cp config.json ./plugin/
	@docker --context=glab rm -vf tmp

create:
	@echo "### remove existing plugin ${PLUGIN_NAME}:${PLUGIN_TAG} if exists"
	@docker --context=glab plugin rm -f ${PLUGIN_NAME}:${PLUGIN_TAG} || true
	@echo "### create new plugin ${PLUGIN_NAME}:${PLUGIN_TAG} from ./plugin"
	@docker --context=glab plugin create ${PLUGIN_NAME}:${PLUGIN_TAG} ./plugin

enable:		
	@echo "### enable plugin ${PLUGIN_NAME}:${PLUGIN_TAG}"		
	@docker --context=glab plugin enable ${PLUGIN_NAME}:${PLUGIN_TAG}

push:  clean rootfs create enable
	@echo "### push plugin ${PLUGIN_NAME}:${PLUGIN_TAG}"
	@docker --context=glab plugin push ${PLUGIN_NAME}:${PLUGIN_TAG}

install:
	@docker --context=glab plugin disable ${PLUGIN_NAME}:${PLUGIN_TAG}
	@docker --context=glab plugin remove ${PLUGIN_NAME}:${PLUGIN_TAG}
	@docker --context=glab plugin install ${PLUGIN_NAME}:${PLUGIN_TAG}
	@docker --context=glab plugin enable ${PLUGIN_NAME}:${PLUGIN_TAG}
