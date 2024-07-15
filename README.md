<p align="center">
  <img src="https://raw.githubusercontent.com/PKief/vscode-material-icon-theme/ec559a9f6bfd399b82bb44393651661b08aaf7ba/icons/folder-markdown-open.svg" width="100" alt="project-logo">
</p>
<p align="center">
    <h1 align="center">FUSIONN</h1>
</p>
<p align="center">
    <em>Code harmony, seamless media flow 🌐️⚡️🚀</em>
</p>
<p align="center">
 <!-- local repository, no metadata badges. -->
<p>
<p align="center">
  <em>Developed with the software and tools below.</em>
</p>
<p align="center">
 <img src="https://img.shields.io/badge/Docker-2496ED.svg?style=default&logo=Docker&logoColor=white" alt="Docker">
 <img src="https://img.shields.io/badge/GitHub%20Actions-2088FF.svg?style=default&logo=GitHub-Actions&logoColor=white" alt="GitHub%20Actions">
 <img src="https://img.shields.io/badge/Go-00ADD8.svg?style=default&logo=Go&logoColor=white" alt="Go">
 <img src="https://img.shields.io/badge/Wire-000000.svg?style=default&logo=Wire&logoColor=white" alt="Wire">
</p>

<br><!-- TABLE OF CONTENTS -->
<details>
  <summary>Table of Contents</summary><br>

- [📍 Overview](#-overview)
- [🧩 Features](#-features)
- [🗂️ Repository Structure](#️-repository-structure)
- [📦 Modules](#-modules)
- [🚀 Getting Started](#-getting-started)
  - [⚙️ Installation](#️-installation)
  - [🤖 Usage](#-usage)
  - [🧪 Tests](#-tests)
- [🛠 Project Roadmap](#-project-roadmap)
- [🤝 Contributing](#-contributing)
- [🎗 License](#-license)
- [🔗 Acknowledgments](#-acknowledgments)

</details>
<hr>

## 📍 Overview

FusionN is a versatile media management application designed to enhance user experiences by seamlessly handling multimedia operations. Key functionalities include data structuring with `ffmpeg.go`, subtitle processing via `subtitle.go`, coordinated processing orchestrated in `processor_set.go`, subtitle merging managed by merge.go, handler organization using handlers_set.go, and constant definitions within consts.go for multilingual support. It utilizes DeepL API translations and third-party notifications services like Apprise. The application adopts a modular architecture with efficient dependency management through Google's wire library, FastHTTP for communication, and automates Docker image building and publishing via GitHub workflows.

---

## 🧩 Features

---

## 🗂️ Repository Structure

```sh
└── fusionn/
    ├── .github
    │   └── workflows
    ├── LICENSE
    ├── Makefile
    ├── README.md
    ├── cmd
    │   └── fusionn
    ├── data
    │   ├── .DS_Store
    │   └── media
    ├── deploy
    │   └── Dockerfile
    ├── go.mod
    ├── go.sum
    ├── internal
    │   ├── app.go
    │   ├── consts
    │   ├── entity
    │   ├── handlers
    │   ├── processor
    │   ├── repository
    │   ├── server.go
    │   └── wire_gen.go
    └── pkg
        ├── apprise.go
        ├── deepl.go
        └── pkg_set.go
```

---

## 📦 Modules

<details closed><summary>.</summary>

| File                 | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ---                  | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| [go.mod](go.mod)     | This `go.mod` file is the central manifest for dependencies of the fusionn project, which defines a software architecture for building an application. The file lists over three dozen external libraries such as Fasthttp, Go-astisub, Bytedance's Sonic, and Google's Wire, indicating the application leverages efficient networking, subtitling tools, audio processing frameworks, and dependency injection mechanisms respectively. By orchestrating these libraries, the fusionn project aims to deliver an advanced media processing solution with adaptive capabilities.                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| [Makefile](Makefile) | The Makefile in this repository provides a setup task that automatically downloads Google Wire, a tool for dependency injection used throughout the project, and generates its associated files with a single command (wire). This promotes consistent and efficient coding within the Fusionn application architecture.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| [go.sum](go.sum)     | User-friendly command executionAllowing users to run various operations on their data by executing appropriate commands from the terminal.2. **Integration with other repository componentsEffectively ties together different modules within the FusionN project, leveraging their functionalities for seamless usage.3.**Parsing command arguments and flagsEnabling fine-tuned control of operation settings, ensuring optimal results for various datasets and use-cases.4. **Error handling and reportingProviding clear feedback to users in case of issues or exceptions during CLI interactions, promoting a user-friendly experience.5.**Help documentationGenerating detailed help information for each command, assisting users with correct syntax, usage examples, and additional tips on best practices.In summary, this CLI is instrumental in making the FusionN software easily accessible to end-users while serving as a unifying layer that interfaces between different components within the larger FusionN project architecture. |

</details>

<details closed><summary>cmd.fusionn</summary>

| File                           | Summary                                                                                                                                                                                                                                                                                          |
| ---                            | ---                                                                                                                                                                                                                                                                                              |
| [main.go](cmd/fusionn/main.go) | The given Go file initiates the command-line execution of Fusionn server, listening on port 4664, ensuring seamless communication within the applications architecture. Its main role lies in setting up the server instance and kickstarting its functionality within this open-source project. |

</details>

<details closed><summary>deploy</summary>

| File                            | Summary                                                                                                                                                                                                                                                                                                                                              |
| ---                             | ---                                                                                                                                                                                                                                                                                                                                                  |
| [Dockerfile](deploy/Dockerfile) | Builds Fusionn (repositorys primary app) using Alpine Linux container. Stages for faster dependency loading. Optimizes with-s-w' flags and copies build output to Docker image. Sets time zone to Asia/Shanghai and installs additional dependencies, including FFMPEG, inside the deployed container. Enables CMD to run Fusionn app on deployment. |

</details>

<details closed><summary>internal</summary>

| File                                | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---                                 | ---                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| [server.go](internal/server.go)     | Bootstraps Go application server with DI (Dependency Injection) using Wire library in this FusionN repositorys internal package, allowing seamless integration between various components like handlers, repositories, and more.                                                                                                                                                                                                               |
| [wire_gen.go](internal/wire_gen.go) | This file `internal/wire_gen.go` generates dependencies for components within the FusionN application, ensuring theyre properly injected and managed during runtime. It ties together modules such as handlers, processors, repositories, and third-party packages like DeepL and Apprise to create a seamless workflow. This enables efficient interplay between application layers while fostering maintainable and scalable code structure. |
| [app.go](internal/app.go)           | Manages core application functionality in Fusionns architecture by creating an app structure for handling API requests, particularly the /api/v1/merge' post operation. The package orchestrates the interaction between handlers, processors, and repositories within Fusionn's internal components, enhancing modularity and maintaining a well-structured application.                                                                      |

</details>

<details closed><summary>internal.repository</summary>

| File                                                           | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---                                                            | ---                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| [ffmpeg.go](internal/repository/ffmpeg.go)                     | The ffmpeg.go file, situated within the `fusionn` projects repository structure, implements an interface for extracting and managing subtitles in videos. It uses external command-line tools like ffprobe and ffmpeg to parse and manipulate video streams, focusing on English, Chinese Simplified, and Traditional subtitle extraction, all while ensuring clean, manageable, and adaptable code within the larger project architecture. |
| [convertor.go](internal/repository/convertor.go)               | Translates subtitles from Traditional Chinese to Simplified Chinese using DeepL API and OpenCC tool for the Fusionn video editing platform. Ensures efficient handling by processing text batches, facilitating seamless localization of content.                                                                                                                                                                                           |
| [repositories_set.go](internal/repository/repositories_set.go) | Orchestrates a dynamic service registry, configuring modules such as Algorithms, Parser, FFmpeg, and Convertor in this applications architecture. Leverages Dependency Injection pattern using wire library to ensure flexible, modular, and testable design for the video processing pipeline.                                                                                                                                             |
| [parser.go](internal/repository/parser.go)                     | In this `parser.go` file, we create a custom subtitle parser. By implementing an interface with the astisub package, our parser can read and analyze input subtitle files. This enables efficient subtitle handling within the FusionN media management system.                                                                                                                                                                             |
| [algo.go](internal/repository/algo.go)                         | This internal repository function, located within `algo.go`, implements a clustering algorithm for merging subtitles. By matching Chinese and English captions based on start time, it groups related text, improving the accuracy and readability of multilingual transcripts in the parent Fusionn project.                                                                                                                               |

</details>

<details closed><summary>internal.repository.common</summary>

| File                                              | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ---                                               | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| [common.go](internal/repository/common/common.go) | The function `GenerateASSSubtitle` creates an ASS subtitle struct with default style attributes, and applies these styles to all items. It first parses an input color in the format `&HColor` (omitting `&H` if present) into an `astisub.Color`. Then, it sets various style attributes such as font name, size, primary and secondary colors, outline color, background color, bold, italic, underline, strikeout, scaling, border style, outlines/shadows, alignment, margin, and encoding. These defaults are used for all items within the generated ASS subtitle structure. The function `ParseASSColor` prepares the input color by trimming the `&H` prefix, padding with zeros if necessary, and then parsing it as a hexadecimal string into individual RGBA components. |

</details>

<details closed><summary>internal.entity</summary>

| File                                     | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ---                                      | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| [request.go](internal/entity/request.go) | Extracts a specified Sonarr episode file path from user requests within FusionN media management application, enriching its ability to seamlessly handle media operations and optimize user experiences.                                                                                                                                                                                                                                                         |
| [ffmpeg.go](internal/entity/ffmpeg.go)   | The `ffmpeg.go` entity file within the `internal/entity` directory serves to structure data obtained from FFprobe, an essential component in the video processing pipeline of the FusionN project. It parses multimedia stream information and stores details such as codec type, resolution, duration, subtitles, and more. These data help optimize media handling during transmission or storage, enhancing overall functionality in the FusionN application. |

</details>

<details closed><summary>internal.processor</summary>

| File                                                    | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ---                                                     | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| [subtitle.go](internal/processor/subtitle.go)           | This file, `internal/processor/subtitle.go`, handles merging, translating, and re-writing subtitles from a media file for the Fusionn application. It integrates multiple components such as parsers, convertors, and translation algorithms to deliver accurate subtitles in simplified Chinese (chs) or English (eng). The merged subtitle is then written to the specified output path and an apprise notification is sent upon successful execution. |
| [processor_set.go](internal/processor/processor_set.go) | Coordinates processing operations within FusionNs core engine. The `processor_set.go` file houses a composition root that orchestrates creation and interaction between essential processing components such as Subtitle instances, adhering to the Dependency Inversion Principle by using Interface Segregation via Googles Wire framework.                                                                                                            |

</details>

<details closed><summary>internal.handlers</summary>

| File                                                 | Summary                                                                                                                                                                                                                                                                               |
| ---                                                  | ---                                                                                                                                                                                                                                                                                   |
| [merge.go](internal/handlers/merge.go)               | Manages subtitle merging for media content within the Fusionn platform by interacting with a specific subtitle processor, ensuring seamless consolidation and a unified user experience.                                                                                              |
| [handlers_set.go](internal/handlers/handlers_set.go) | The `internal/handlers/handlers_set.go` file acts as a registry within the FusionN project. It initializes and organizes Handlers, essential components that process user requests, by employing Dependency Injection with Googles wire library for smooth application functionality. |

</details>

<details closed><summary>internal.consts</summary>

| File                                     | Summary                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ---                                      | ---                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| [command.go](internal/consts/command.go) | This file defines and exports a constant URL `CMDDeepLTranslate` used by the `fusionn` application for DeepL API translations. The `internal/consts` package consolidates application-specific URLs within the fusionn repository, streamlining access to third-party APIs like DeepL and maintaining a modular architecture.                                                                                                                                                   |
| [consts.go](internal/consts/consts.go)   | This code file, located within the consts package, defines a set of global constants for use across the project. It outlines various language codes (Traditional Chinese, Simplified Chinese, and English) and associated titles, as well as specific patterns for handling timecode formats. Additionally, it specifies an external API address (Apprise). These values are crucial for facilitating multilingual support and notifications within the project's architecture. |

</details>

<details closed><summary>.github.workflows</summary>

| File                                                       | Summary                                                                                                                                                                                                                                                                                                                |
| ---                                                        | ---                                                                                                                                                                                                                                                                                                                    |
| [docker-publish.yml](.github/workflows/docker-publish.yml) | In this GitHub repository, titled fusionn, the provided file, `docker-publish.yml`, orchestrates automated Docker image building and publishing events. This workflow is a crucial part of the Continuous Integration (CI) process in the repository, enabling efficient and seamless distribution of the application. |

</details>

<details closed><summary>pkg</summary>

| File                         | Summary                                                                                                                                                                                                                                                                                                                                                                    |
| ---                          | ---                                                                                                                                                                                                                                                                                                                                                                        |
| [apprise.go](pkg/apprise.go) | Enables external communication through the `apprise` interface, offering a function `SendBasicMessage` for delivering JSON data via POST requests to specified URLs using FastHTTP, ultimately facilitating seamless interaction with third-party services within the FusionN application ecosystem.                                                                       |
| [pkg_set.go](pkg/pkg_set.go) | Converts and initializes application dependencies using wire, binding `deepL` and `apprise` instances within the main package for efficient use throughout the software. This fosters a modular design and streamlined dependency management within the fusionn application.                                                                                               |
| [deepl.go](pkg/deepl.go)     | The `pkg/deepl.go` module acts as an interface for translating text within the Fusionn application, providing multilingual support by leveraging DeepLs API services. By instantiating this package, developers can effortlessly translate user-provided content between multiple languages, ensuring seamless cross-cultural communication in our diverse user community. |

</details>

---

## 🚀 Getting Started

**System Requirements:**

- **Go**: `version x.y.z`

### ⚙️ Installation

<h4>From <code>source</code></h4>

> 1. Clone the fusionn repository:
>
> ```console
> $ git clone ../fusionn
> ```
>
> 2. Change to the project directory:
>
> ```console
> $ cd fusionn
> ```
>
> 3. Install the dependencies:
>
> ```console
> $ go build -o myapp
> ```

### 🤖 Usage

<h4>From <code>source</code></h4>

> Run fusionn using the command below:
>
> ```console
> $ ./myapp
> ```

### 🧪 Tests

> Run the test suite using the command below:
>
> ```console
> $ go test
> ```

---

## 🛠 Project Roadmap

- [X] `► INSERT-TASK-1`
- [ ] `► INSERT-TASK-2`
- [ ] `► ...`

---

## 🤝 Contributing

Contributions are welcome! Here are several ways you can contribute:

- **[Report Issues](https://local/fusionn/issues)**: Submit bugs found or log feature requests for the `fusionn` project.
- **[Submit Pull Requests](https://local/fusionn/blob/main/CONTRIBUTING.md)**: Review open PRs, and submit your own PRs.
- **[Join the Discussions](https://local/fusionn/discussions)**: Share your insights, provide feedback, or ask questions.

<details closed>
<summary>Contributing Guidelines</summary>

1. **Fork the Repository**: Start by forking the project repository to your local account.
2. **Clone Locally**: Clone the forked repository to your local machine using a git client.

   ```sh
   git clone ../fusionn
   ```

3. **Create a New Branch**: Always work on a new branch, giving it a descriptive name.

   ```sh
   git checkout -b new-feature-x
   ```

4. **Make Your Changes**: Develop and test your changes locally.
5. **Commit Your Changes**: Commit with a clear message describing your updates.

   ```sh
   git commit -m 'Implemented new feature x.'
   ```

6. **Push to local**: Push the changes to your forked repository.

   ```sh
   git push origin new-feature-x
   ```

7. **Submit a Pull Request**: Create a PR against the original project repository. Clearly describe the changes and their motivations.
8. **Review**: Once your PR is reviewed and approved, it will be merged into the main branch. Congratulations on your contribution!

</details>

<details closed>
<summary>Contributor Graph</summary>
<br>
<p align="center">
   <a href="https://local{/fusionn/}graphs/contributors">
      <img src="https://contrib.rocks/image?repo=fusionn">
   </a>
</p>
</details>

---

## 🎗 License

This project is protected under the [SELECT-A-LICENSE](https://choosealicense.com/licenses) License. For more details, refer to the [LICENSE](https://choosealicense.com/licenses/) file.

---

## 🔗 Acknowledgments

- List any resources, contributors, inspiration, etc. here.

[**Return**](#-overview)

---
