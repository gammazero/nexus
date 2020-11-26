
import {AuthobanClient} from "./client"

function main() {

    let arithmeticServer = new AuthobanClient({realm:"nexus.examples",port:8080})

    let arrayToSum = [1,2,3,4,5]

    arithmeticServer.rpc("sum",arrayToSum).then(result=>{
        console.log("The sum of ",arrayToSum," is ",result)

    }).catch(err=>{
        console.log("Ooops, something went wrong:", err)

    })

    arithmeticServer.rpc("div",[1,2]).then(result=>{
        console.log("The div of ",arrayToSum," is ",result)

    }).catch(err=>{
        console.log("Ooops, something went wrong:", err)

    })


}

main()
