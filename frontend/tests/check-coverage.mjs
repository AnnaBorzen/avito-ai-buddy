import {readFileSync,readdirSync,writeFileSync} from 'node:fs';
import {join,relative,isAbsolute} from 'node:path';
import {fileURLToPath} from 'node:url';
const root=fileURLToPath(new URL('../',import.meta.url));
const report=JSON.parse(readFileSync(join(root,'coverage','coverage-summary.json'),'utf8'));
const byFile=Object.fromEntries(Object.entries(report).filter(([path])=>path!=='total').map(([path,metrics])=>[(isAbsolute(path)?relative(root,path):path).replaceAll('\\','/'),metrics]));
const paths=new Set(Object.keys(byFile));
function check(folder){
  for(const e of readdirSync(join(root,folder),{withFileTypes:true})){
    const path=folder+'/'+e.name;
    if(e.isDirectory()) check(path);
    else if(/\.(tsx?|mjs)$/.test(path) && !path.endsWith('.d.ts') && !paths.has(path))throw Error('Отсутствует в отчёте покрытия: '+path);
  }
}
check('src');check('server');
if(!report.total.lines.total || report.total.lines.pct<=80)throw Error('Требуется более 80% строк.');
writeFileSync(join(root,'coverage','coverage-summary.json'),JSON.stringify({total:report.total,...byFile},null,2)+'\n');
console.log('Покрыто строк: '+report.total.lines.covered+' / '+report.total.lines.total+' ('+report.total.lines.pct+'%).');
