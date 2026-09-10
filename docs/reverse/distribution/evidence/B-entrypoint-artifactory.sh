# 来源: SOURCE B 运行时容器 /entrypoint-artifactory.sh（docker exec artifactory cat，只读取证）
# 版本: 7.161.20 | 证据等级: E4 | 原样摘录（全文）
# 用途: Docker 发行形态的启动序/引导复制/优雅停机/6.x 迁移触发行为规格依据
# 注: C 包（7.161.16 zip）不含此文件——它是 docker 镜像层资产；C 的对应物是 app/bin/artifactory.sh

#!/bin/bash
#
# An entrypoint script for Artifactory to allow custom setup before server starts
#
: ${ARTIFACTORY_NAME:=artifactory}
: ${JF_PRODUCT_HOME:=/opt/jfrog/artifactory}
: ${ARTIFACTORY_BOOTSTRAP:=/artifactory_bootstrap}
: ${BOOTSTRAP:=/bootstrap}

JF_ARTIFACTORY_PID=${JF_PRODUCT_HOME}/var/work/run/${ARTIFACTORY_NAME}.pid
. ${JF_PRODUCT_HOME}/app/bin/installerCommon.sh
ARTIFACTORY_BIN_FOLDER=${JF_PRODUCT_HOME}/app/bin

sourceScript(){
    local fileName=$1

    [ ! -z "${fileName}" ] || errorExit "Target file is not set"
    [ ! -f "${fileName}" ] || errorExit "${fileName} file is not found"
    source "${fileName}"   || errorExit "Unable to source ${fileName}, please check if the $USER user has permissions to perform this action"
}

initHelpers(){
    local systemYamlHelper="${ARTIFACTORY_BIN_FOLDER}"/systemYamlHelper.sh
    local installerCommon="${ARTIFACTORY_BIN_FOLDER}"/installerCommon.sh
    local artCommon="${ARTIFACTORY_BIN_FOLDER}"/artifactoryCommon.sh

    export YQ_PATH="${ARTIFACTORY_BIN_FOLDER}/../third-party/yq"
    sourceScript "${systemYamlHelper}"
    sourceScript "${installerCommon}"
    sourceScript "${artCommon}"

    export JF_SYSTEM_YAML="${JF_PRODUCT_HOME}/var/etc/system.yaml"
}

createDirectory () {
    [[ -d "$1" ]] || mkdir -p $1 || errorExit "Could not create dir $1"
}

# Print on container startup information about Dockerfile location
printDockerFileLocation() {
    logger "Dockerfile for this image can found inside the container."
    logger "To view the Dockerfile: 'cat /docker/artifactory-pro/Dockerfile.artifactory'."
}

copyArtifactoryBootstrapFiles () {
    if [[ -d "${ARTIFACTORY_BOOTSTRAP}" ]]; then
        logger "Copying Artifactory bootstrap files"
        copyFilesNoOverwrite "${ARTIFACTORY_BOOTSTRAP}" "${JF_PRODUCT_HOME}/var/etc/${ARTIFACTORY_NAME}"
    fi
}

copyBootstrapFiles () {
    if [[ -d "${BOOTSTRAP}" ]]; then
        logger "Copying bootstrap files"
        copyFilesNoOverwrite "${BOOTSTRAP}" "${JF_PRODUCT_HOME}/var/etc"
    fi
}

performGracefulShutdown() {
    getSystemValue "artifactory.port" "8081"
    local artifactoryPort=$YAML_VALUE
    local endpoint="http://localhost:${artifactoryPort}/artifactory/api/v1/system/preStop"
    echo "Notifying Artifactory about termination via: ${endpoint}"
    local statusMessage=$(curl -sS "${endpoint}")
    echo "Artifactory graceful shutdown finished with message: ${statusMessage}"
}

terminate () {
    echo -e "\nTerminating Artifactory"
    performGracefulShutdown
    ${JF_PRODUCT_HOME}/app/bin/artifactory.sh stop
}

backupFile() {
    local sourceFile=$1
    local destinationFile=$2

    if [ -f "${sourceFile}" ]; then

        yes | \cp  -f "${sourceFile}" "${destinationFile}" || warn "Could not copy [${sourceFile}] to [${destinationFile}]"

        chown -R "${JF_USER}":"${JF_USER}" "${destinationFile}"  || warn "Setting ownership of [${destinationFile}] to [${JF_USER}:${JF_USER}] failed"
    fi
}

#Triggering migration
dockerMigration() {
    if [[ -z "$ENABLE_MIGRATION" ]]; then
        return
    fi
    if [[ $ENABLE_MIGRATION =~ (y|Y) ]]; then
        if [ -f "${JF_PRODUCT_HOME}/app/bin/migrate.sh" ]; then
            logger "Triggering migration script. This will migrate if needed and may take some time."
            logger "Migration logs will be available at [${JF_PRODUCT_HOME}/app/bin/migration.log]. The file will be archived at [${JF_PRODUCT_HOME}/var/log]"
            export JF_ROOT_DATA_DIR="/var/opt/jfrog/artifactory"
            export JF_USER="${ARTIFACTORY_NAME}"
            bash ${JF_PRODUCT_HOME}/app/bin/migrate.sh >/dev/null || errorExit "Aborting configuration since migration has failed"
        else
            errorExit "Skipping migration because ${JF_PRODUCT_HOME}/app/bin/migrate.sh is missing"
        fi
            backupFile "${JF_PRODUCT_HOME}/app/bin/migration.log" "${JF_PRODUCT_HOME}/var/log/migration.log"
    fi
}

# Set libaio path
setLibaioPath() {
    local libaioPath=${JF_PRODUCT_HOME}/app/artifactory/libaio
    getSystemValue "$SYS_KEY_SHARED_DATABASE_TYPE" "NOT_SET" "false" "$JF_SYSTEM_YAML"
    DATABASE_TYPE="$YAML_VALUE"
    if [[ "${DATABASE_TYPE}" == "${SYS_KEY_SHARED_DATABASE_TYPE_VALUE_ORACLE}" ]]; then
        getYamlValue "shared.env.LD_LIBRARY_PATH" "$JF_SYSTEM_YAML" "false"
        local yamlLdLibraryPath="$YAML_VALUE"
        if [[ -z "${yamlLdLibraryPath}" ]]; then
            setSystemValue "shared.env.LD_LIBRARY_PATH" "${libaioPath}" "$JF_SYSTEM_YAML"
        else
            [[ "${yamlLdLibraryPath}" != *"${libaioPath}"* ]] && setSystemValue "shared.env.LD_LIBRARY_PATH" "${libaioPath}:${yamlLdLibraryPath}" "$JF_SYSTEM_YAML"
        fi
    fi
}

# Catch Ctrl+C and other termination signals to try graceful shutdown
trap terminate SIGINT SIGTERM SIGHUP

logger "Preparing to run Artifactory in Docker"
logger "Running as $(id)"


printDockerFileLocation

initHelpers
commonStartupActions "docker" 2>/dev/null
dockerMigration
setLibaioPath

# Wait for DB
# On slow systems, when working with docker-compose, the DB container might be up,
# but not ready to accept connections when Artifactory is already trying to access it.
waitForDB
[ $? -eq 0 ] || errorExit "Database failed to start in the given time"

copyArtifactoryBootstrapFiles
copyBootstrapFiles

# Run Artifactory as JF_ARTIFACTORY_USER user
exec ${JF_PRODUCT_HOME}/app/bin/artifactory.sh &
art_pid=$!

if [ -n "$JF_ARTIFACTORY_PID" ];
then
    mkdir -p $(dirname "$JF_ARTIFACTORY_PID") || \
        errorExit "Could not create dir for $JF_ARTIFACTORY_PID";
fi

echo "${art_pid}" > ${JF_ARTIFACTORY_PID}

wait ${art_pid}
