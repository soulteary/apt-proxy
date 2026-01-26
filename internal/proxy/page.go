package proxy

import (
	"strings"
)

const SERVER_DEFAULT_TEMPLATE = `<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="original ui design" content="https://dribbble.com/shots/833870-Rebound-Twitter">
    <title>soulteary/apt-proxy</title>
</head>

<body>
    <style>
        .clearfix:before,
        .clearfix:after {
            content: "";
            display: table;
        }

        .clearfix:after {
            clear: both;
        }

        body {
            background: #343434;
            background-size: cover;
            color: #4c4c4c;
            font: 300 1.1em/1.7em 'HelveticaNeue-Light', Helvetica, Arial, sans-serif;
        }

        body>p {
            bottom: 10px;
            color: #fff;
            font-size: 12px;
            left: 10px;
            position: absolute;
        }

        body>p a {
            color: #fff
        }

        .container {
            color: #4c4c4c;
            height: 249px;
            left: 50%;
            margin: -125px 0 0 -223px;
            position: absolute;
            top: 50%;
            width: 446px;
            box-shadow: 1px 1px 16px rgba(0, 0, 0, .58);
            border-radius: 12px;
        }

        .header {
            color: #fdfdfd;
            font: 12px/17px 'HelveticaNeue-UltraLight', Helvetica, Arial, sans-serif;
            padding: 18px 20px 20px;
            border-radius: 2px 2px 0 0;
        }

        .logo {
            border: 4px solid #c9c9c9;
            border-top-width: 3px;
            float: left;
            margin-right: 16px;
            position: relative;
            box-shadow: 0 1px 1px rgba(0, 0, 0, .6), 0 -1px 0 #9ce5fa;
            border-radius: 2px
        }

        .logo::before {
            content: "";
            position: absolute;
            top: 0;
            bottom: 0;
            left: 0;
            right: 0;
            box-shadow: 0 1px 3px rgba(0, 0, 0, .4) inset;
            -moz-box-shadow: 0 1px 3px rgba(0, 0, 0, .4) inset;
            -webkit-box-shadow: 0 1px 3px rgba(0, 0, 0, .4) inset;
        }

        .logo img {
            display: block;
            box-shadow: 0 0 2px rgba(0, 0, 0, .5) inset
        }

        .header h2,
        .header p {
            float: left;
            margin-top: 30px;
        }

        .header h2 {
            font-size: 26px;
            line-height: 32px;
            margin: 10px 0 8px
        }

        .header h2 a {
            color: #fdfdfd;
            text-decoration: none;
        }

        .header h2 a:hover,
        .logo:hover {
            opacity: 0.6;
        }

        .stats {
            background: rgb(243, 243, 243);
            background: -moz-linear-gradient(top, rgba(243, 243, 243, 1) 0%, rgba(236, 236, 237, 1) 100%);
            background: -webkit-gradient(linear, left top, left bottom, color-stop(0%, rgba(243, 243, 243, 1)), color-stop(100%, rgba(236, 236, 237, 1)));
            background: -webkit-linear-gradient(top, rgba(243, 243, 243, 1) 0%, rgba(236, 236, 237, 1) 100%);
            background: -o-linear-gradient(top, rgba(243, 243, 243, 1) 0%, rgba(236, 236, 237, 1) 100%);
            background: -ms-linear-gradient(top, rgba(243, 243, 243, 1) 0%, rgba(236, 236, 237, 1) 100%);
            background: linear-gradient(to bottom, rgba(243, 243, 243, 1) 0%, rgba(236, 236, 237, 1) 100%);
            filter: progid:DXImageTransform.Microsoft.gradient(startColorstr='#f3f3f3', endColorstr='#ececed', GradientType=0);
            border-top: 1px solid #fff;
            border-bottom: 1px solid #d4d4d4;
        }

        .stat {
            color: #4c4c4c;
            float: left;
            font-size: 14px;
            line-height: 17px;
            padding: 15px 0 14px;
            text-align: center;
            text-decoration: none;
            text-shadow: 0 1px 0 #fff;
            width: 148px;
            cursor: default;
        }

        .stat:first-child {
            margin-left: 1px
        }

        .stat.enable:hover {
            color: #747474;
            cursor: pointer;
        }

        .stat strong {
            display: block;
            font-size: 25px;
            line-height: 25px
        }

        .stats-bottom {
            border-radius: 10px;
            border-top-left-radius: 0;
            border-top-right-radius: 0;
        }
    </style>

    <div class="container">
        <div class="header clearfix">
            <a href="https://github.com/soulteary/apt-proxy" target="_blank" class="logo">
                <img width="80"
                    src="data:image/svg+xml;base64,PD94bWwgdmVyc2lvbj0iMS4wIiBlbmNvZGluZz0iVVRGLTgiIHN0YW5kYWxvbmU9Im5vIj8+PCFET0NUWVBFIHN2ZyBQVUJMSUMgIi0vL1czQy8vRFREIFNWRyAxLjEvL0VOIiAiaHR0cDovL3d3dy53My5vcmcvR3JhcGhpY3MvU1ZHLzEuMS9EVEQvc3ZnMTEuZHRkIj48c3ZnIHdpZHRoPSIxMDAlIiBoZWlnaHQ9IjEwMCUiIHZpZXdCb3g9IjAgMCAxMDY3IDEwNjciIHZlcnNpb249IjEuMSIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIiB4bWxuczp4bGluaz0iaHR0cDovL3d3dy53My5vcmcvMTk5OS94bGluayIgeG1sOnNwYWNlPSJwcmVzZXJ2ZSIgeG1sbnM6c2VyaWY9Imh0dHA6Ly93d3cuc2VyaWYuY29tLyIgc3R5bGU9ImZpbGwtcnVsZTpldmVub2RkO2NsaXAtcnVsZTpldmVub2RkO3N0cm9rZS1saW5lam9pbjpyb3VuZDtzdHJva2UtbWl0ZXJsaW1pdDoyOyI+PHJlY3QgeD0iNDE1LjQ2OSIgeT0iNzM4LjE0NiIgd2lkdGg9IjI0Mi4wNTIiIGhlaWdodD0iNDkuOTE3IiBzdHlsZT0iZmlsbDojYjdiMmFlO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik0zMTQuMTI1LDI5Ny4zOTZsLTE4LjA4MywxMTEuMjcxbDUyLjAyMSw4OC4wODNsMjEuMTU2LC02NS4yODFsLTU1LjA5NCwtMTM0LjA3M1oiIHN0eWxlPSJmaWxsOiM0NzJkMjQ7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTM2OS4yMTksNDMxLjQ2OWwtMjEuMTU2LDY1LjI4MWwxNDkuODEyLDI4Mi41NTJsMTIuNzE5LDBsLTE0MS4zNzUsLTM0Ny44MzNaIiBzdHlsZT0iZmlsbDojZWJlYWU0O2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik0zMTQuMTI1LDI5Ny4zOTZsNzYuNTgzLDMwLjQyN2wyOC4yODIsMTA4LjM3NWwtNDkuNzcxLC00LjcyOWwtNTUuMDk0LC0xMzQuMDczWiIgc3R5bGU9ImZpbGw6IzM1MWYxYTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNDE4Ljk5LDQzNi4xOThsLTQ5Ljc3MSwtNC43MjlsMTQxLjM3NSwzNDcuODMzbDExLjAyMSwwbC0xMDIuNjI1LC0zNDMuMTA0WiIgc3R5bGU9ImZpbGw6I2FmYjBhYTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNDI0Ljg0NCwyMjUuNTYzbC0zOC43MTksOTkuNjU2bDMyLjg2NSwxMTAuOTc5bDMzLjA3MywtNTkuNzI5bC0yNy4yMTksLTE1MC45MDdaIiBzdHlsZT0iZmlsbDojNDcyZDI0O2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik00NTIuMDYzLDM3Ni40NjlsLTMzLjA3Myw1OS43MjlsOTQuMTg3LDM0My4xMDRsMTAuMTA0LDBsLTcxLjIxOSwtNDAyLjgzM1oiIHN0eWxlPSJmaWxsOiNlYmVhZTQ7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTQyNC44NDQsMjI1LjU2M2w2Ny43Niw1OS45MjdsNi4zNjUsMTE1LjQ2OGwtNDYuOTA2LC0yNC40ODlsLTI3LjIxOSwtMTUwLjkwN1oiIHN0eWxlPSJmaWxsOiMzNTFmMWE7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTQ5OC45NjksNDAwLjk1OGwtNDYuOTA2LC0yNC40ODlsNzEuMjE4LDQwMi44MzNsOS4xMzYsMGwtMzMuNDQ4LC0zNzguMzQ0WiIgc3R5bGU9ImZpbGw6I2FmYjBhYTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNTQxLjU3MywyMDMuNTczbC01NS4yNzEsODEuNzZsMTIuNjY3LDExNS42MjVsNDEuODMzLC00MS40MjdsMC43NzEsLTE1NS45NThaIiBzdHlsZT0iZmlsbDojNDcyZDI0O2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik01NDAuODAyLDM1OS41MzFsLTQxLjgzMyw0MS40MjdsMjkuNTEsMzc4LjM0NGw4Ljg1NCwwbDMuNDY5LC00MTkuNzcxWiIgc3R5bGU9ImZpbGw6I2ViZWFlNDtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNTQxLjU3MywyMDMuNTczbDU0LjY4Nyw4My4yNzFsLTE0LjU0MSwxMTMuODk2bC00MC45MTcsLTQxLjIwOWwwLjc3MSwtMTU1Ljk1OFoiIHN0eWxlPSJmaWxsOiMzNTFmMWE7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTU4MS43MTksNDAwLjc0bC00MC45MTcsLTQxLjIwOWwtMy40NjksNDE5Ljc3MWw4LjgwMiwwbDM1LjU4NCwtMzc4LjU2MloiIHN0eWxlPSJmaWxsOiNhZmIwYWE7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTY2MS4xMDQsMjI4LjYzNWwtNjguNjQ2LDU2LjcxOWwtMTAuNzM5LDExNS4zODZsNDkuMDYyLC0yMi40MjhsMzAuMzIzLC0xNDkuNjc3WiIgc3R5bGU9ImZpbGw6IzQ3MmQyNDtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNjMwLjc4MSwzNzguMzEzbC00OS4wNjIsMjIuNDI3bC00Mi45MjcsMzc4LjU2Mmw5LjI2LDBsODIuNzI5LC00MDAuOTg5WiIgc3R5bGU9ImZpbGw6I2ViZWFlNDtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNjYxLjEwNCwyMjguNjM1bDM2LjgwMiwxMDEuNzcxbC0zNS42MDQsMTAzLjc2MWwtMzEuNTIxLC01NS44NTRsMzAuMzIzLC0xNDkuNjc4WiIgc3R5bGU9ImZpbGw6IzM1MWYxYTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNjMwLjc4MSwzNzguMzEzbC04Mi43MjksNDAwLjk4OWw5LjY1NiwwbDEwNC41OTQsLTM0NS4xMzVsLTMxLjUyMSwtNTUuODU0WiIgc3R5bGU9ImZpbGw6I2FmYjBhYTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNzY4LjQxNywyOTkuMjA4bC03Ni42MzYsMjguMTM2bC0zMC4wNzMsMTA3LjM1NGw0OS44MzQsLTIuOTc5bDU2Ljg3NSwtMTMyLjUxMVoiIHN0eWxlPSJmaWxsOiM0NzJkMjQ7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTY2MS43MDgsNDM0LjY5OGwtMTExLjUxLDM0NC42MDRsMTAuNSwwbDE1MC44NDQsLTM0Ny41ODNsLTQ5LjgzNCwyLjk3OVoiIHN0eWxlPSJmaWxsOiNlYmVhZTQ7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTc2OC40MTcsMjk5LjIwOGwxNi42NTYsMTEyLjI4MmwtNTMuNDc5LDg2LjIxOGwtMjAuMDUyLC02NS45ODlsNTYuODc1LC0xMzIuNTExWiIgc3R5bGU9ImZpbGw6IzM1MWYxYTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNzExLjU0Miw0MzEuNzE5bC0xNTAuODQ0LDM0Ny41ODNsMTIuMjkyLDBsMTU4LjYwNCwtMjgxLjU5NGwtMjAuMDUyLC02NS45ODlaIiBzdHlsZT0iZmlsbDojYWZiMGFhO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik0zNzEuNDI3LDM4OC40NDhjLTAuNTczLDEwLjQxNyAtOS4xMTUsMTguMzg1IC0xOS4wODMsMTcuODEybC0zLjIwOSwtMC4xODdjLTkuOTY4LC0wLjYyNSAtMTcuNTgzLC05LjU2MyAtMTcsLTE5Ljk5bDAuMTU3LC0zLjM3NWMwLjU4MywtMTAuNDE2IDkuMTI1LC0xOC40MTYgMTkuMDkzLC0xNy44MTJsMy4xOTgsMC4xODdjOS45NjksMC42MDQgMTcuNTg0LDkuNTUyIDE3LjAxMSwyMGwtMC4xNjcsMy4zNjVaIiBzdHlsZT0iZmlsbDojYzE3NTIxO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik0zNjguMzEzLDM4Ni40NThjLTAuNTczLDEwLjQxNyAtOS4xMjUsMTguNDI3IC0xOS4wOTQsMTcuODAybC0zLjIwOSwtMC4xNjZjLTkuOTU4LC0wLjYxNSAtMTcuNTcyLC05LjU2MyAtMTcsLTIwbDAuMTg4LC0zLjM2NWMwLjU3MywtMTAuNDE3IDkuMTE1LC0xOC40MTcgMTkuMDk0LC0xNy44MTJsMy4yMDgsMC4yMDhjOS45NjksMC41OTQgMTcuNTgzLDkuNTUyIDE3LjAxLDE5Ljk5bC0wLjE5NywzLjM0M1oiIHN0eWxlPSJmaWxsOiNmMmEwNDA7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTQ2NS43NCwzNDAuMjcxYy0wLjU3MywxMC40MTcgLTkuMTI1LDE4LjQxNyAtMTkuMDg0LDE3LjgwMmwtMy4yMDgsLTAuMTg4Yy05Ljk3OSwtMC42MTQgLTE3LjU4MywtOS41NjIgLTE3LjAxLC0xOS45NzlsMC4xNzcsLTMuMzc1YzAuNTgzLC0xMC40MTYgOS4xMzUsLTE4LjQwNiAxOS4wOTMsLTE3LjgxMmwzLjE5OCwwLjE5OGM5Ljk2OSwwLjYwNCAxNy41ODQsOS41NTIgMTcsMTkuOTc5bC0wLjE2NiwzLjM3NVoiIHN0eWxlPSJmaWxsOiNjMTc1MjE7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTQ2Mi42MTUsMzM4LjI4MWMtMC41NzMsMTAuNDE3IC05LjEyNSwxOC40MTcgLTE5LjA5NCwxNy44MDJsLTMuMTk4LC0wLjE4N2MtOS45NTgsLTAuNjA0IC0xNy41NzMsLTkuNTYzIC0xNywtMjBsMC4xODcsLTMuMzY1YzAuNTczLC0xMC40MTYgOS4xMTUsLTE4LjQwNiAxOS4wODQsLTE3LjgxMmwzLjIwOCwwLjIwOGM5Ljk2OSwwLjYwNCAxNy41ODMsOS41NTIgMTcuMDEsMjAuMDFsLTAuMTk3LDMuMzQ0WiIgc3R5bGU9ImZpbGw6I2YyYTA0MDtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNTYxLjkxNywzMjAuMDYzYy0wLjU3MywxMC40MTYgLTkuMTE1LDE4LjQxNiAtMTkuMDg0LDE3LjgyMmwtMy4yMDgsLTAuMTg3Yy05Ljk2OSwtMC42MTUgLTE3LjU5NCwtOS41NjMgLTE3LjAxLC0xOS45NzlsMC4xODcsLTMuMzc1YzAuNTczLC0xMC40MTcgOS4xMjUsLTE4LjQxNyAxOS4wOTQsLTE3LjgxM2wzLjIwOCwwLjIwOWM5Ljk2OSwwLjYwNCAxNy41ODMsOS41NDEgMTcsMTkuOTc5bC0wLjE4NywzLjM0NFoiIHN0eWxlPSJmaWxsOiNjMTc1MjE7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTU1OC44MDIsMzE4LjA3M2MtMC41NjIsMTAuNDE3IC05LjExNCwxOC40MTcgLTE5LjA5NCwxNy43OTJsLTMuMTk4LC0wLjE3OGMtOS45NjgsLTAuNjE0IC0xNy41ODMsLTkuNTYyIC0xNywtMjBsMC4xODgsLTMuMzU0YzAuNTczLC0xMC40MTYgOS4xMTUsLTE4LjQxNiAxOS4wODMsLTE3LjgxMmwzLjIwOSwwLjIwOGM5Ljk2OCwwLjU3MyAxNy41NzMsOS41NTIgMTcuMDEsMTkuOTlsLTAuMTk4LDMuMzU0WiIgc3R5bGU9ImZpbGw6I2YyYTA0MDtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNjYyLjQzOCwzMzUuMjgxYy0wLjU3MywxMC40MTcgLTkuMTE1LDE4LjQxNyAtMTkuMDg0LDE3LjgwMmwtMy4yMDgsLTAuMTc3Yy05Ljk2OSwtMC42MTQgLTE3LjU4NCwtOS41NjIgLTE3LC0xOS45ODlsMC4xODcsLTMuMzY1YzAuNTg0LC0xMC40MTcgOS4xMjUsLTE4LjQxNyAxOS4wOTQsLTE3LjgxMmwzLjE5OCwwLjE5N2M5Ljk2OSwwLjYwNSAxNy41ODMsOS41NTMgMTcsMjBsLTAuMTg4LDMuMzQ0WiIgc3R5bGU9ImZpbGw6I2MxNzUyMTtmaWxsLXJ1bGU6bm9uemVybzsiLz48cGF0aCBkPSJNNjU5LjMyMywzMzMuMjcxYy0wLjU4MywxMC40MTYgLTkuMTI1LDE4LjQyNyAtMTkuMTA0LDE3LjgxMmwtMy4yMDksLTAuMTg3Yy05Ljk1OCwtMC42MjUgLTE3LjU3MiwtOS41NjMgLTE3LC0yMGwwLjE4OCwtMy4zNTRjMC41ODMsLTEwLjQxNyA5LjEyNSwtMTguNDI3IDE5LjA4MywtMTcuODEzbDMuMjA5LDAuMTk4YzkuOTY4LDAuNTk0IDE3LjU4Myw5LjU2MyAxNy4wMSwyMGwtMC4xNzcsMy4zNDRaIiBzdHlsZT0iZmlsbDojZjJhMDQwO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik03NTIuNzA4LDM4Ny40MzhjLTAuNTczLDEwLjQxNiAtOS4xMTQsMTguNDA2IC0xOS4wODMsMTcuODIybC0zLjE5OCwtMC4yMDhjLTkuOTY5LC0wLjYwNCAtMTcuNTk0LC05LjU2MiAtMTcuMDEsLTE5Ljk3OWwwLjE3NywtMy4zNzVjMC41ODMsLTEwLjQxNyA5LjEyNSwtMTguNDA2IDE5LjA5MywtMTcuODEzbDMuMjA5LDAuMTg4YzkuOTY5LDAuNjA0IDE3LjU4Myw5LjU1MiAxNywxOS45OWwtMC4xODgsMy4zNzVaIiBzdHlsZT0iZmlsbDojYzE3NTIxO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik03NDkuNTk0LDM4NS40MTdjLTAuNTczLDEwLjQxNiAtOS4xMjUsMTguNDI3IC0xOS4wOTQsMTcuODAybC0zLjE5OCwtMC4xNzdjLTkuOTY5LC0wLjYyNSAtMTcuNTgzLC05LjU2MyAtMTcuMDEsLTIwbDAuMTk4LC0zLjM2NWMwLjU3MiwtMTAuNDE3IDkuMTA0LC0xOC40MTcgMTkuMDgzLC0xNy44MTJsMy4xOTgsMC4xODdjOS45NjksMC42MDQgMTcuNTgzLDkuNTUyIDE3LjAyMSwyMGwtMC4xOTgsMy4zNjVaIiBzdHlsZT0iZmlsbDojZjJhMDQwO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik00MTUuNTgzLDY1Ni4zNTRsMjQyLjAyMSwwbDAsNDYuMzQ0bC0yNDIuMDIxLC0wbDAsLTQ2LjM0NFptLTAuMTE0LDk5Ljc2MWwyNDIuMTM1LC0wbDAsMjMuMTg3bC0yNDIuMTM1LDBsLTAsLTIzLjE4N1oiIHN0eWxlPSJmaWxsOiNjNDE4MmY7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHJlY3QgeD0iNDE1LjU4MyIgeT0iNzAyLjY5OCIgd2lkdGg9IjI0Mi4wMjEiIGhlaWdodD0iNTMuNDE3IiBzdHlsZT0iZmlsbDojZDhkNGQwO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik00MTUuNTgzLDY1Ni4zNTRsMTIyLjM4NiwwbC0wLDQ2LjM0NGwtMTIyLjM4NiwtMGwwLC00Ni4zNDRabS0wLjExNCw5OS43NjFsMTIzLjMxMiwtMGwwLDIzLjE4N2wtMTIzLjMxMiwwbC0wLC0yMy4xODdaIiBzdHlsZT0iZmlsbDojZjM1MTUzO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxyZWN0IHg9IjQ1Ni4yMDgiIHk9IjY1Ni43MjkiIHdpZHRoPSIzOC44OTYiIGhlaWdodD0iNDUuOTY5IiBzdHlsZT0iZmlsbDojYzQxODJmO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxyZWN0IHg9IjU3OS4xMjUiIHk9IjY1Ni41NDIiIHdpZHRoPSIzOC44OTYiIGhlaWdodD0iNDUuOTY5IiBzdHlsZT0iZmlsbDojZjM1MTUzO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik00NzUuNjQ2LDcwMi41MWwtNTkuODY1LDI2Ljg5Nmw1OS44NjUsMjYuNzA5bDI0LjIwOCwtMGwtNTYuNjA0LC0yNi43MDlsNTYuNjA0LC0yNi43MDgiIHN0eWxlPSJmaWxsOiMzY2FiZWU7ZmlsbC1ydWxlOm5vbnplcm87Ii8+PHBhdGggZD0iTTU1MC43NCw3MDguMDk0bC0xMi42MTUsLTUuNDA3bC0wLjMxMiwwbC0xMi4yNzEsNS40MDdsLTQ3LjQxNywyMS4zMTJsNDcuNTk0LDIxLjIxOWwxMi45MzcsNS40NzlsMC4zMDIsMGwxMS42MzYsLTUuNDc5bDQ3LjU5MywtMjEuMjE5bC00Ny40NDcsLTIxLjMxMlptLTEyLjYwNSwzNi42OThsLTMyLjU1MiwtMTUuMzU1bDMyLjU1MiwtMTUuMzU0bDMyLjU2MywxNS4zNTVsLTMyLjU2MywxNS4zNTRaIiBzdHlsZT0iZmlsbDojYzQxODJmO2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik01MjUuNTgzLDcwMi41MWwwLjE0NiwwbC0wLjE0NiwwWm0wLjMwMiwwbC0wLjE1NiwwbDAuMTU2LDBaIiBzdHlsZT0iZmlsbDojNDBhM2M2O2ZpbGwtcnVsZTpub256ZXJvOyIvPjxwYXRoIGQ9Ik01OTcuNjU2LDcwMi42MDRsNTkuODY1LDI2Ljg5NmwtNTkuODY1LDI2LjcwOGwtMjQuMjA4LDBsNTYuNjE1LC0yNi43MDhsLTU2LjYxNSwtMjYuNjk4IiBzdHlsZT0iZmlsbDojMGQ1MjlmO2ZpbGwtcnVsZTpub256ZXJvOyIvPjwvc3ZnPg=="
                    alt="APT Proxy"></a>
            <h2><a href="https://github.com/soulteary/apt-proxy" target="_blank">soulteary/apt-proxy</a></h2>
            <p>APT Proxy is a Lightweight and Reliable packages (Ubuntu / Debian / CentOS / Alpine) cache tool, supports a large
                number of common system and Docker usage.</p>
        </div>

        <div class="stats clearfix">
            <a href="#" class="stat">
                <strong>$APT_PROXY_CACHE_SIZE</strong>
                Cache size
            </a>
            <a href="#" class="stat">
                <strong>$APT_PROXY_FILE_NUMBER</strong>
                Number of files
            </a>
            <a href="#" class="stat">
                <strong>$APT_PROXY_AVAILABLE_SIZE</strong>
                Available space
            </a>
        </div>

        <div class="stats stats-bottom clearfix">
            <a href="#" class="stat">
                <strong>$APT_PROXY_MEMORY_USAGE</strong>
                Memory usage
            </a>
            <a href="#" class="stat">
                <strong>$APT_PROXY_GOROUTINES</strong>
                Goroutines
            </a>
            <a href="https://github.com/soulteary/apt-proxy/issues" class="stat enable" target="_blank">
                <strong>GitHub</strong>
                Feedback
            </a>
        </div>
    </div>

</body>

</html>`

func GetBaseTemplate(cacheSize string, filesNumber string, availableSize string,
	memoryUsage string, goroutines string) string {

	tpl := strings.Replace(SERVER_DEFAULT_TEMPLATE, "$APT_PROXY_CACHE_SIZE", cacheSize, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_FILE_NUMBER", filesNumber, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_AVAILABLE_SIZE", availableSize, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_MEMORY_USAGE", memoryUsage, 1)
	tpl = strings.Replace(tpl, "$APT_PROXY_GOROUTINES", goroutines, 1)

	return tpl
}
