import {createRequire} from 'node:module';import fs from 'node:fs';import path from 'node:path';import {fileURLToPath} from 'node:url';import crypto from 'node:crypto';
const C=process.env.HARMONY_GENERATED_CONTEXT;if(!C)throw new Error('Set HARMONY_GENERATED_CONTEXT');const E=path.dirname(fileURLToPath(import.meta.url));const req=createRequire(C+'/apps/web/package.json');
const {createServer}=await import(req.resolve('vite'));const vue=(await import(req.resolve('@vitejs/plugin-vue'))).default;const tailwind=(await import(req.resolve('@tailwindcss/vite'))).default;
for(const f of ['review-files.html','review-files.ts'])fs.copyFileSync(E+'/'+f,C+'/apps/web/'+f);
const entries=[];const received=[];fs.mkdirSync(E+'/received',{recursive:true});
const receiver={name:'isolated-synthetic-multipart-receiver',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 if(!req.url?.startsWith('/api/'))return next();
 try{
  if(req.method==='GET'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify(req.url==='/api/review-received'?received:{entries}));return;}
  if(req.method!=='POST'||!req.url.includes('/container/fs/upload'))throw new Error('Unexpected fixture request');
  const data=[];for await(const part of req)data.push(part);const bytes=Buffer.concat(data);
  const form=await new Response(bytes,{headers:{'Content-Type':req.headers['content-type']}}).formData();const file=form.get('file');const name=file?.name;
  if(!['first.bin','second.txt','third.dat'].includes(name))throw new Error('Unexpected synthetic file');
  const actual=Buffer.from(await file.arrayBuffer());const expected=fs.readFileSync(E+'/samples/'+name);
  if(!actual.equals(expected)||form.get('path')!=='/data/'+name||!req.url.includes('/bots/10000000-0000-4000-8000-000000000001/'))throw new Error('Synthetic bytes or scope mismatch');
  fs.writeFileSync(E+'/received/'+name,actual);received.push({name,bytes:actual.length,sha256:crypto.createHash('sha256').update(actual).digest('hex'),destination:form.get('path'),originalBot:true});fs.writeFileSync(E+'/http-received.json',JSON.stringify(received,null,2)+'\n');entries.push({name,path:form.get('path'),isDir:false,size:actual.length});res.setHeader('Content-Type','application/json');res.end('{}');
 }catch(error){console.error(error.message);res.statusCode=500;res.end('Synthetic receiver check failed');}
 });}};
const server=await createServer({configFile:false,root:C+'/apps/web',plugins:[receiver,vue(),tailwind()],resolve:{dedupe:['vue','pinia'],alias:[{find:'@/store/chat-list',replacement:E+'/chat-store.ts'},{find:'@/store/workspace-tabs',replacement:E+'/tabs-store.ts'},{find:'#',replacement:C+'/packages/ui/src'},{find:'@',replacement:C+'/apps/web/src'}]},server:{host:'127.0.0.1',port:18953,strictPort:true,fs:{allow:[C,E,fs.realpathSync(C+'/node_modules')]}}});await server.listen();console.log('isolated F04 component server ready 18953');
