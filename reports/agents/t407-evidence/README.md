# t407-evidence — AQL/搜索域 t226 活体核验证据（API 文本实录）

- 会话：2026-09-01；实例 t226-artifactory OSS 7.84.10 rev 78410900（API http://172.16.58.129:8181/artifactory/api；凭据仅会话内使用——admin 口令取 VM ~/t228-admin-pw，零落盘）
- 方法：差集法只读探针（INC-1）——仅 GET 与 POST /search/aql 只读查询；零写操作、零 delete/update/dryRun 动词、零建仓/建属性；容器栈抵达时即 Up（T-381 保留态），核验后维持 Up 不变（零副作用）
- 命名：vNN-主题.txt = 响应体逐字节实录（curl 原样）；状态码/响应头记录见 aql.md §错误面/§老搜索 各表

t226-artifactory Up 22 hours
t226-pg Up 22 hours
