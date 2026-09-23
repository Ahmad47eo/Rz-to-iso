let inputFile=null;
let outputAccess=null;
let inputReader=null;
let cache=null;
let cacheStart=0;
let readBuffer=null;
const OUT_NAME='rvz-output.iso';
const CACHE_SIZE=4*1024*1024;

async function initFiles(file,outputName){
  if(!file) throw new Error('No RVZ file was supplied.');
  inputFile=file;
  inputReader=new FileReaderSync();
  cache=null;
  cacheStart=0;

  const root=await navigator.storage.getDirectory();
  const oh=await root.getFileHandle(outputName,{create:true});
  outputAccess=await oh.createSyncAccessHandle();
  outputAccess.truncate(0);

  globalThis.rvzInputSize=()=>inputFile.size;
  globalThis.rvzInputRead=(offset,length)=>{
    if(length<=0)return new Uint8Array(0);
    const start=Number(offset);
    if(!Number.isFinite(start)||start<0||start>=inputFile.size)return new Uint8Array(0);

    const wanted=Math.min(Number(length),inputFile.size-start);
    if(wanted<=0)return new Uint8Array(0);

    if(!readBuffer || readBuffer.byteLength<wanted){
      readBuffer=new Uint8Array(wanted);
    }

    let copied=0;
    while(copied<wanted){
      const pos=start+copied;

      if(!cache || pos<cacheStart || pos>=cacheStart+cache.byteLength){
        cacheStart=Math.floor(pos/CACHE_SIZE)*CACHE_SIZE;
        const cacheEnd=Math.min(inputFile.size,cacheStart+CACHE_SIZE);
        cache=new Uint8Array(inputReader.readAsArrayBuffer(inputFile.slice(cacheStart,cacheEnd)));
      }

      const from=pos-cacheStart;
      const take=Math.min(wanted-copied,cache.byteLength-from);
      if(take<=0)break;

      readBuffer.set(cache.subarray(from,from+take),copied);
      copied+=take;
    }

    return readBuffer.subarray(0,copied);
  };

  globalThis.rvzOutputWrite=(data,offset)=>outputAccess.write(data,{at:Number(offset)});
  globalThis.rvzProgress=(done,total)=>postMessage({type:'progress',done,total});
}

let wasmReady=false;
(async()=>{
  try{
    importScripts('./wasm_exec.js');
    const go=new Go();
    const result=await WebAssembly.instantiateStreaming(fetch('./rvz.wasm'),go.importObject);
    go.run(result.instance);
    wasmReady=true;
    postMessage({type:'ready'});
  }catch(e){
    postMessage({type:'error',message:'Decoder failed to load: '+(e.message||String(e))});
  }
})();

onmessage=async e=>{
  const m=e.data||{};
  if(m.type!=='start'||!wasmReady)return;
  try{
    await initFiles(m.file,OUT_NAME);
    postMessage({type:'reading'});
    const result=globalThis.rvzConvert();
    if(result.error)throw new Error(result.error);
    outputAccess.flush();
    outputAccess.close();
    outputAccess=null;
    inputFile=null;
    inputReader=null;
    cache=null;
    readBuffer=null;
    postMessage({type:'done',size:result.size,name:m.name});
  }catch(e){
    try{if(outputAccess)outputAccess.close()}catch(_){}
    outputAccess=null;
    inputFile=null;
    inputReader=null;
    cache=null;
    readBuffer=null;
    postMessage({type:'error',message:e.message||String(e)});
  }
};