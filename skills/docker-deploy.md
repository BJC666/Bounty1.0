---
name: docker-deploy
description: 容器化部署最佳实践（镜像最小化、非 root、健康检查）
triggers: [Docker, 容器, 部署, 镜像, compose, k8s]
read_only: false
---
# 容器化部署规范

- 镜像最小化：多阶段构建（编译与运行分离）；选 alpine/distroless 类基础镜像；安装包锁版本。
- 非 root 运行：镜像内创建专用用户并以该用户执行应用（避免默认管理员权限运行）。
- 配置注入：环境变量只放非敏感配置；敏感配置走挂载密钥/密钥管理服务，禁止打进镜像层。
- 健康检查：容器声明 healthcheck；编排层按健康状态滚动发布与自动重启。
- 资源与日志：声明 CPU/内存限额；日志输出到 stdout/stderr 交给采集层；数据放命名卷并备份。
