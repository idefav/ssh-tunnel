#!/bin/bash
# 下载最新版本的SSH Tunnel服务
# 该脚本会自动检测系统架构并下载对应的服务包

# 设置颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# 确定操作系统
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    "darwin")
        OS="darwin" # macOS
        ;;
    "linux")
        OS="linux"
        ;;
    *)
        echo -e "${RED}不支持的操作系统: $OS${NC}"
        exit 1
        ;;
esac

# 确定架构
ARCH="$(uname -m)"
case "$ARCH" in
    "x86_64")
        GOARCH="amd64"
        ;;
    "amd64")
        GOARCH="amd64"
        ;;
    "arm64" | "aarch64")
        GOARCH="arm64"
        ;;
    "i386" | "i686")
        GOARCH="386"
        ;;
    *)
        echo -e "${RED}不支持的架构: $ARCH${NC}"
        exit 1
        ;;
esac

echo -e "${GREEN}检测到系统: $OS, 架构: $GOARCH${NC}"

# 创建临时目录用于下载
TEMP_DIR="/tmp/ssh-tunnel-download"
mkdir -p "$TEMP_DIR"

# GitHub API URL 获取最新版本
API_URL="https://api.github.com/repos/idefav/ssh-tunnel/releases/latest"
echo -e "${YELLOW}正在获取最新版本信息...${NC}"

# 检查是否安装了curl或wget
if command -v curl &> /dev/null; then
    DOWNLOAD_CMD="curl -L"
    GET_CMD="curl -s"
elif command -v wget &> /dev/null; then
    DOWNLOAD_CMD="wget -O"
    GET_CMD="wget -q -O-"
else
    echo -e "${RED}错误: 需要安装 curl 或 wget${NC}"
    exit 1
fi

# 获取最新版本信息
RELEASE_INFO=$(${GET_CMD} ${API_URL})
if [ $? -ne 0 ]; then
    echo -e "${RED}获取版本信息失败${NC}"
    exit 1
fi

# 解析JSON (使用简单的grep和cut方法，如果可用，最好使用jq)
VERSION=$(echo "$RELEASE_INFO" | grep -o '"tag_name": *"[^"]*"' | cut -d'"' -f4)
echo -e "${GREEN}找到最新版本: $VERSION${NC}"

# 资产文件名
ASSET_NAME="ssh-tunnel-svc-${OS}-${GOARCH}"

# 查找下载URL
DOWNLOAD_URL=$(echo "$RELEASE_INFO" | grep -o "\"browser_download_url\": *\"[^\"]*${ASSET_NAME}[^\"]*\"" | cut -d'"' -f4)

if [ -z "$DOWNLOAD_URL" ]; then
    echo -e "${RED}错误: 未找到适合当前系统的安装包 ($ASSET_NAME)${NC}"
    exit 1
fi

OUTPUT_FILE="${TEMP_DIR}/${ASSET_NAME}"
echo -e "${YELLOW}正在下载 $ASSET_NAME...${NC}"

# 下载文件
if [[ "$DOWNLOAD_CMD" == "curl -L" ]]; then
    curl -L -o "$OUTPUT_FILE" "$DOWNLOAD_URL"
else
    wget -O "$OUTPUT_FILE" "$DOWNLOAD_URL"
fi

if [ $? -ne 0 ]; then
    echo -e "${RED}下载失败${NC}"
    exit 1
fi

# 创建bin目录（如果不存在）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${SCRIPT_DIR}/bin"
mkdir -p "$BIN_DIR"

# 复制文件到bin目录
DEST_FILE="${BIN_DIR}/${ASSET_NAME}"
cp "$OUTPUT_FILE" "$DEST_FILE"
chmod +x "$DEST_FILE"

echo -e "${GREEN}下载完成! 文件已保存到: $DEST_FILE${NC}"

# 清理临时文件
rm -rf "$TEMP_DIR"
