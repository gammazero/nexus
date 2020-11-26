
import {WampClient} from "./client"

function main() {

    let sumServer = new WampClient({realm:"nexus.examples",port:8080})

    let arrayToSum = [1,2,3,4,5]

    sumServer.rpc("sum",arrayToSum).then(result=>{
        console.log("The sum of ",arrayToSum," is ",result)

    }).catch(err=>{
        console.log("Ooops, something went wrong:", err)

    })

}

main()
