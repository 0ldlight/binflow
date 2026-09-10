# 来源: SOURCE C 安装包 /Users/lzw/Downloads/artifactory-pro-7.161.16/app/bin/artifactory.default
# 版本: 7.161.16 | 证据等级: E2 | 原样摘录（全文）
# 用途: JVM 默认参数与 JF_PRODUCT_HOME/TOMCAT_HOME 默认值的行为规格依据

#!/bin/bash

##########################################################################################################################
#
#   NOTE : THIS FILE SHOULD NOT BE EDITED! Any customisations should come from <JF_PRODUCT_HOME>/var/etc/system.yaml
#
##########################################################################################################################

#Default values
export JF_PRODUCT_HOME=${JF_PRODUCT_HOME:-/opt/jfrog/artifactory}

export TOMCAT_HOME=$JF_PRODUCT_HOME/app/artifactory/tomcat

DEFAULT_JAVA_OPTIONS="-server -Xms512m -Xmx2g -XX:+UseG1GC -XX:OnOutOfMemoryError=\"kill -9 %p\""
DEFAULT_JAVA_OPTIONS="${DEFAULT_JAVA_OPTIONS} --add-opens java.base/java.util=ALL-UNNAMED --add-opens java.base/java.lang.reflect=ALL-UNNAMED --add-opens java.base/java.lang.invoke=ALL-UNNAMED --add-opens java.base/java.text=ALL-UNNAMED --add-opens java.base/java.nio=ALL-UNNAMED --add-opens java.desktop/java.awt.font=ALL-UNNAMED --add-opens java.naming/com.sun.jndi.ldap=ALL-UNNAMED"
DEFAULT_JAVA_OPTIONS="${DEFAULT_JAVA_OPTIONS} -Dfile.encoding=UTF8 -Djruby.compile.invokedynamic=false -Djruby.bytecode.version=1.8"
DEFAULT_JAVA_OPTIONS="${DEFAULT_JAVA_OPTIONS} -Djava.security.egd=file:/dev/./urandom"
DEFAULT_JAVA_OPTIONS="${DEFAULT_JAVA_OPTIONS} -XX:+EnableDynamicAgentLoading"
DEFAULT_JAVA_OPTIONS="${DEFAULT_JAVA_OPTIONS} -Dartdist=zip -Djf.product.home=${JF_PRODUCT_HOME}"

# Avoid recursive append of same options
if [ -z "${JAVA_OPTIONS}" ] || [ ! -z "${JAVA_OPTIONS##*${UNIQUE_DEFAULT_JAVA_OPTIONS}*}" ]; then
    export JAVA_OPTIONS="${JAVA_OPTIONS} ${DEFAULT_JAVA_OPTIONS}"
fi
