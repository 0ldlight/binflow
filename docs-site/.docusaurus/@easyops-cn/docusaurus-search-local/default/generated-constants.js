import lunr from "/Users/lzw/dev-center/docs-site/node_modules/lunr/lunr.js";
require("/Users/lzw/dev-center/docs-site/node_modules/lunr-languages/lunr.stemmer.support.js")(lunr);
require("/Users/lzw/dev-center/docs-site/node_modules/@easyops-cn/docusaurus-search-local/dist/client/shared/lunrLanguageZh.js").lunrLanguageZh(lunr);
require("/Users/lzw/dev-center/docs-site/node_modules/lunr-languages/lunr.multi.js")(lunr);
export const removeDefaultStopWordFilter = [];
export const language = ["zh","en"];
export const searchIndexUrl = "search-index{dir}.json";
export const searchResultLimits = 8;
export const fuzzyMatchingDistance = 1;