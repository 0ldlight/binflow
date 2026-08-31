/// <reference types="vite/client" />

// 品牌资产以 `?url` 形态消费（vite 内联阈值内转 data URI——零额外请求，
// gzip 后计入 SPA 预算；FR-126/T-389）。vite/client 补齐该导入类型。
