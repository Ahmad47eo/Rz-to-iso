let inputFile=null;
let outputAccess=null;
let inputReader=null;
let inputCache=null;
let inputCacheStart=-1;
let inputRoot=null;

const OUT_NAME='rvz-output.iso';
const INPUT_NAME='rvz-input.cache';
const CACHE_SIZE=2*1024*1024;
const MAX_READ_SIZE=CACHE_SIZE;

async function initFiles(file,outputName){
  if(!file) throw new Error('No RVZ file was supplied.');

  inputFile=file;
  inputReader=new FileReaderSync();
  inputCache=null;
  inputCacheStart=-1;

  inputRoot=await navigator.storage.getDirectory();

  const oh=await inputRoot.getFileHandle(outputName,{create:true});
  outputAccess=await oh.createSyncAccessHandle();
  outputAccess.truncate(0);

  globalThis.rvzInputSize=()=>inputFile.size;

  globalThis.rvzInputRead=(offset,length)=>{
    if(length<=0)return new Uint8Array(0);

    const start=Number(offset);
    const requested=Math.min(Number(length),MAX_READ_SIZE);
    if(!Number.isFinite(start)||!Number.isFinite(requested)||start<0||start>=inputFile.size||requested<=0){
      return new Uint8Array(0);
    }

    const blockStart=Math.floor(start/CACHE_SIZE)*CACHE_SIZE;

    if(!inputCache || inputCacheStart!==blockStart){
      const end=Math.min(inputFile.size,blockStart+CACHE_SIZE);
      inputCache=new Uint8Array(
        inputReader.readAsArrayBuffer(inputFile.slice(blockStart,end))
      );
      inputCacheStart=blockStart;
    }

    const from=start-inputCacheStart;
    const available=inputCache.byteLength-from;
    if(available<=0)return new Uint8Array(0);

    const take=Math.min(requested,available);
    return inputCache.subarray(from,from+take);
  };

  globalThis.rvzOutputWrite=(data,offset)=>outputAccess.write(data,{at:Number(offset)});
}

let wasmReady=false;

(async()=>{
  try{
    importScripts('./wasm_exec.js');

    const go=new Go();
    const result=await WebAssembly.instantiateStreaming(
      fetch('./rvz.wasm?v=fix20260923',{cache:'reload'}),
      go.importObject
    );

    go.run(result.instance);
    wasmReady=true;
    postMessage({type:'ready'});
  }catch(e){
    postMessage({
      type:'error',
      message:'Decoder failed to load: '+(e.message||String(e))
    });
  }
})();

function cleanup(){
  inputFile=null;
  inputReader=null;
  inputCache=null;
  inputCacheStart=-1;

  try{
    if(outputAccess)outputAccess.close();
  }catch(_){}
  outputAccess=null;
}

onmessage=async e=>{
  const m=e.data||{};
  if(m.type!=='start'||!wasmReady)return;

  try{
    await initFiles(m.file,OUT_NAME);

    const started=globalThis.rvzStart();
    if(!started || started===undefined){
      throw new Error('WASM startup returned no result.');
    }
    if(started.error)throw new Error(started.error);

    const total=started.size;
    let stepCount=0;
    let lastStepMs=0;

    postMessage({type:'started',total});

    const runStep=()=>{
      const stepStarted=performance.now();

      try{
        const result=globalThis.rvzStep();
        lastStepMs=performance.now()-stepStarted;
        stepCount++;

        if(!result || result===undefined){
          throw new Error('Decoder returned no result; the WASM runtime stopped.');
        }

        if(result.error)throw new Error(result.error);

        if(result.done){
          outputAccess.flush();
          outputAccess.close();
          outputAccess=null;
          postMessage({type:'done',size:result.size,name:m.name});
          cleanup();
          return;
        }

        postMessage({
          type:'progress',
          done:result.size,
          total,
          step:stepCount,
          stepMs:Math.round(lastStepMs)
        });

        setTimeout(runStep,0);
      }catch(err){
        cleanup();
        postMessage({
          type:'error',
          message:err.message||String(err),
          step:stepCount,
          stepMs:Math.round(lastStepMs)
        });
      }
    };

    runStep();
  }catch(e){
    cleanup();
    postMessage({type:'error',message:e.message||String(e)});
  }
};