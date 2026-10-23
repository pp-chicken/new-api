#!/usr/bin/env bash
# 使用项目 Dockerfile 构建前端、后端和最终运行镜像。
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
image="new-api"
tag=""
platform="linux/amd64"
push=false
dry_run=false
proxy=""
export_image=true
save_only=false
output=""
build_options=()

usage() {
  cat <<'EOF'
用法：./build-docker.sh [选项]

默认构建 linux/amd64 镜像并加载到本地 Docker，包含当前工作区的代码。
本地构建成功后自动导出 .tar.gz 镜像包，默认保存到项目 artifacts/docker/。
构建前请启动 Docker，并确保已安装 Buildx；本机无需安装 Go 或 Bun。

选项：
  --image NAME       镜像仓库名称，默认 new-api（不包含标签）
  --tag TAG          镜像标签，默认使用时间、Git 提交号和架构
  --platform VALUE   目标平台，默认 linux/amd64；支持 linux/arm64
                     多平台使用逗号分隔，且必须同时指定 --push
  --push             构建后推送到镜像仓库，不加载到本地 Docker
                     推送模式不生成本地镜像包
  --output FILE      自定义镜像包路径，必须以 .tar.gz 结尾
                     相对路径以执行命令时的目录为基准；不覆盖已有文件
  --no-export        仅构建并加载镜像，不导出镜像包
  --save-only        仅打包已有本地镜像，必须通过 --tag 指定标签
  --no-cache         禁用构建缓存
  --pull             检查并拉取 Dockerfile 指定的基础镜像
  --proxy URL        为 Docker CLI 的认证请求指定 HTTP/HTTPS 代理
                     例如 http://127.0.0.1:7890；镜像下载仍使用 Docker 的代理配置
  --dry-run          只打印构建命令，不执行构建或推送
  -h, --help         显示帮助

示例：
  ./build-docker.sh
  ./build-docker.sh --tag local-test --platform linux/arm64
  ./build-docker.sh --proxy http://127.0.0.1:7890
  ./build-docker.sh --save-only --tag network-check-20261023
  ./build-docker.sh --tag v1 --output /tmp/new-api-v1.tar.gz
  ./build-docker.sh --image registry.example.com/team/new-api --tag v1 --push
  ./build-docker.sh --image registry.example.com/team/new-api --tag v1 \
    --platform linux/amd64,linux/arm64 --push

推送前请自行通过 docker login 登录目标仓库。
镜像标签不会改写项目 VERSION 文件。
EOF
}

fail() {
  printf '错误：%s\n' "$*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image|--tag|--platform|--proxy|--output)
      [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || fail "$1 缺少参数值"
      case "$1" in
        --image) image="$2" ;;
        --tag) tag="$2" ;;
        --platform) platform="$2" ;;
        --proxy) proxy="$2" ;;
        --output) output="$2" ;;
      esac
      shift 2
      ;;
    --push) push=true; shift ;;
    --no-export) export_image=false; shift ;;
    --save-only) save_only=true; shift ;;
    --no-cache|--pull) build_options+=("$1"); shift ;;
    --dry-run) dry_run=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "未知选项：$1；使用 --help 查看帮助" ;;
  esac
done

if [[ "$save_only" == true ]]; then
  [[ -n "$tag" ]] || fail "--save-only 必须通过 --tag 指定已有镜像标签"
  [[ "$push" == false && "$export_image" == true ]] || fail "--save-only 不能与 --push 或 --no-export 同用"
fi
if [[ "$push" == true ]]; then
  export_image=false
fi
[[ -z "$output" || "$export_image" == true ]] || fail "--output 不能与 --push 或 --no-export 同用"

if [[ -n "$proxy" ]]; then
  case "$proxy" in
    http://?*|https://?*) ;;
    *) fail "--proxy 必须使用 http:// 或 https:// 地址" ;;
  esac
  # 代理仅应用于当前脚本进程，不修改 Docker Desktop 或系统配置。
  export HTTP_PROXY="$proxy" HTTPS_PROXY="$proxy" http_proxy="$proxy" https_proxy="$proxy"
fi

[[ -f "$project_dir/Dockerfile" ]] || fail "项目目录中找不到 Dockerfile"
[[ "$image" != -* && "$image" != *[[:space:]]* && "$image" != *@* && "${image##*/}" != *:* ]] \
  || fail "--image 必须是未带标签或摘要的镜像仓库名称"

case "$platform" in
  linux/amd64) arch="amd64" ;;
  linux/arm64) arch="arm64" ;;
  linux/amd64,linux/arm64|linux/arm64,linux/amd64) arch="multi" ;;
  *) fail "--platform 支持 linux/amd64、linux/arm64 或两者的逗号分隔列表" ;;
esac
[[ "$arch" != multi || "$push" == true ]] || fail "多平台构建请指定 --push；本地加载请只选择一个平台"

if [[ -z "$tag" ]]; then
  revision=""
  if command -v git >/dev/null 2>&1; then
    revision="$(git -C "$project_dir" rev-parse --short=12 HEAD 2>/dev/null || true)"
    if [[ -n "$revision" && -n "$(git -C "$project_dir" status --porcelain --untracked-files=normal)" ]]; then
      revision="${revision}-dirty"
    fi
  fi
  tag="$(date '+%Y%m%d-%H%M%S')-${revision:-local}-${arch}"
fi
[[ "$tag" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$ && ${#tag} -le 128 ]] \
  || fail "镜像标签格式不合法，或长度超过 128 个字符"

image_ref="${image}:${tag}"
if [[ "$export_image" == true ]]; then
  output="${output:-$project_dir/artifacts/docker/${image##*/}-${tag}.tar.gz}"
  [[ "$output" == *.tar.gz ]] || fail "--output 必须以 .tar.gz 结尾"
  [[ "$output" == /* ]] || output="$PWD/$output"
  [[ ! -e "$output" && ! -L "$output" ]] || fail "输出文件已存在，请更换 --output：$output"
fi
command_args=(docker buildx build --file "$project_dir/Dockerfile"
  --platform "$platform" --tag "$image_ref" --progress plain)
if [[ "$push" == true ]]; then
  command_args+=(--push)
else
  command_args+=(--load)
fi
# 兼容 macOS 自带 Bash 3.2 在 set -u 下对空数组的处理。
if [[ ${#build_options[@]} -gt 0 ]]; then
  command_args+=("${build_options[@]}")
fi
command_args+=("$project_dir")

printf '镜像：%s\n' "$image_ref"
if [[ "$save_only" == false ]]; then
  printf '平台：%s\n' "$platform"
fi
if [[ -n "$proxy" ]]; then
  printf '已为 Docker CLI 启用指定代理（地址不输出，避免泄露代理凭据）。\n'
fi
if [[ "$save_only" == false ]]; then
  printf '构建命令：'
  printf '%q ' "${command_args[@]}"
  printf '\n'
fi
if [[ "$export_image" == true ]]; then
  printf '镜像包：%s\n' "$output"
fi
if [[ "$dry_run" == true ]]; then
  exit 0
fi

command -v docker >/dev/null 2>&1 || fail "未找到 docker 命令，请先安装 Docker"
docker info >/dev/null 2>&1 || fail "无法连接 Docker，请先启动 Docker 或检查当前 context"
if [[ "$export_image" == true ]]; then
  command -v gzip >/dev/null 2>&1 || fail "未找到 gzip，无法压缩镜像包"
fi

if [[ "$save_only" == false ]]; then
  docker buildx version >/dev/null 2>&1 || fail "Docker Buildx 不可用，请先安装或启用 Buildx"
  if ! "${command_args[@]}"; then
    printf '%s\n' '若日志包含 auth.docker.io/token 超时，可使用 --proxy http://127.0.0.1:7890（请替换为实际代理地址），并检查 Docker Desktop 的代理配置。' >&2
    fail "镜像构建失败，请检查上方 Docker 日志"
  fi
fi

if [[ "$push" == true ]]; then
  printf '\n镜像已推送：%s\n' "$image_ref"
elif [[ "$save_only" == false ]]; then
  printf '\n镜像已构建并加载到本地 Docker：%s\n' "$image_ref"
  printf '查看镜像：docker image inspect %q\n' "$image_ref"
fi

if [[ "$export_image" == true ]]; then
  docker image inspect "$image_ref" >/dev/null 2>&1 || fail "本地镜像不存在：$image_ref"
  mkdir -p -- "$(dirname -- "$output")"
  # 临时包与目标文件放在同一目录，失败或中断时清理，避免留下不完整的成品。
  archive_tmp="$(mktemp "${output}.tmp.XXXXXX")"
  trap 'rm -f -- "$archive_tmp"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  printf '\n正在导出并压缩镜像……\n'
  if ! docker image save "$image_ref" | gzip -1 > "$archive_tmp"; then
    fail "镜像导出失败，本地镜像仍保留"
  fi
  gzip -t "$archive_tmp" || fail "镜像压缩包完整性检查失败"
  # 用硬链接原子创建目标文件，防止并发打包覆盖已有文件。
  ln -- "$archive_tmp" "$output" || fail "无法保存镜像包，目标文件可能已存在：$output"
  rm -f -- "$archive_tmp"
  trap - EXIT INT TERM
  printf '镜像包已生成：%s\n' "$output"
  printf '导入命令：docker load -i %q\n' "$output"
fi
