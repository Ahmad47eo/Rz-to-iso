let inputFile=null;
let outputAccess=null;
let inputReader=null;
let cache=null;
let cacheStart=0;
let readBuffer=null;
const OUT_NAME='rvz-output.iso';
const CACHE_SIZE=1*1024*1024;
const MAX_READ_SIZE=CACHE_SIZE;
let stepCount=0;
let lastStepMs=0;

async function initFiles(file,outputName){
  if(!file) throw new Error('No RVZ file was supplied.');
  inputFile=file;
  inputReader=new FileReaderSync();
  cache=null;
  cacheStart=0;
  readBuffer=null;

  const root=await navigator.storage.getDirectory();
  const oh=await root.getFileHandle(outputName,{create:true});
  outputAccess=await oh.createSyncAccessHandle();
  outputAccess.truncate(0);

  globalThis.rvzInputSize=()=>inputFile.size;
  globalThis.rvzInputRead=(offset,length)=>{
    if(length<=0)return new Uint8Array(0);
    const start=Number(offset);
    if(!Number.isFinite(start)||start<0||start>=inputFile.size)return new Uint8Array(0);

    const wanted=Math.min(Number(length),inputFile.size-start,MAX_READ_SIZE);
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

function cleanup(){
  inputFile=null;
  inputReader=null;
  cache=null;
  readBuffer=null;
}

onmessage=async e=>{
  const m=e.data||{};
  if(m.type!=='start'||!wasmReady)return;

  try{
    await initFiles(m.file,OUT_NAME);
    postMessage({type:'reading'});

    const started=globalThis.rvzStart();
    if(started.error)throw new Error(started.error);

    const total=started.size;
    stepCount=0;
    lastStepMs=0;
    postMessage({type:'started',total});
    const runStep=()=>{
      const stepStarted=performance.now();
      try{
        const result=globalThis.rvzStep();
        lastStepMs=performance.now()-stepStarted;
        stepCount++;

        if(result.error)throw new Error(result.error);

        if(result.done){
          outputAccess.flush();
          outputAccess.close();
          outputAccess=null;
          cleanup();
          postMessage({type:'done',size:result.size,name:m.name});
          return;
        }

        postMessage({type:'progress',done:result.size,total});
        setTimeout(runStep,0);
      }catch(err){
        try{if(outputAccess)outputAccess.close()}catch(_){}
        outputAccess=null;
        cleanup();
        postMessage({type:'error',message:err.message||String(err)});
      }
    };

    runStep();
  }catch(e){
    try{if(outputAccess)outputAccess.close()}catch(_){}
    outputAccess=null;
    cleanup();
    postMessage({type:'error',message:e.message||String(e)});
  }
};