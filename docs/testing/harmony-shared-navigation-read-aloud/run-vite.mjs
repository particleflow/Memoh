import {createRequire} from 'node:module';import fs from 'node:fs';
const C=process.env.HARMONY_GENERATED_CONTEXT;if(!C)throw new Error('Set HARMONY_GENERATED_CONTEXT');const req=createRequire(C+'/apps/web/package.json');const {createServer}=await import(req.resolve('vite'));
const server=await createServer({root:C+'/apps/web',configFile:C+'/apps/web/vite.config.ts',server:{host:'127.0.0.1',port:18954,strictPort:true,fs:{allow:[C,fs.realpathSync(C+'/node_modules')]}}});await server.listen();console.log('Current Harmony source context ready18954');
